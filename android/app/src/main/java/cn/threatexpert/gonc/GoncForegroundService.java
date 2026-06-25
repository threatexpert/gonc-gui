package cn.threatexpert.gonc;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;

import androidx.annotation.Nullable;
import androidx.core.app.NotificationCompat;
import androidx.core.app.ServiceCompat;
import androidx.core.content.ContextCompat;

import java.util.ArrayList;
import java.util.EnumMap;
import java.util.List;
import java.util.Map;

/**
 * Single shared foreground service for the in-process modules (send / receive /
 * VPN server). Each of those runs a gonc session inside the app process; this
 * service keeps the process alive and visible while at least one is active, and
 * renders one aggregated ongoing notification (with download progress when the
 * receive module supplies it).
 *
 * <p>The VPN <em>client</em> is intentionally NOT handled here — it owns
 * {@link GoncVpnService} (a {@link android.net.VpnService}), because a Service
 * cannot be both a VpnService and a plain one. So at most two notifications are
 * ever shown: this one plus the VPN client's.
 */
public final class GoncForegroundService extends Service {

    /** Modules whose sessions run in the app process and rely on this service. */
    enum Module {SEND, RECEIVE, VPN_SERVER}

    /** Connection-status dot shown before a module's line in the notification. */
    enum Dot {GREEN, YELLOW, RED}

    /** A module's contribution to the notification: a status dot plus optional progress. */
    static final class State {
        final Dot dot;
        /** -1 = no determinate progress, 0..100 = progress bar. */
        final int progress;

        State(Dot dot, int progress) {
            this.dot = dot;
            this.progress = progress;
        }

        @Override
        public boolean equals(Object o) {
            if (this == o) {
                return true;
            }
            if (!(o instanceof State)) {
                return false;
            }
            State other = (State) o;
            return dot == other.dot && progress == other.progress;
        }

        @Override
        public int hashCode() {
            return dot.hashCode() * 31 + progress;
        }
    }

    static final String ACTION_REFRESH = "cn.threatexpert.gonc.FOREGROUND_REFRESH";

    private static final int NOTIFICATION_ID = 2002;
    private static final String CHANNEL_ID = "gonc_transfer";

    private static final Object LOCK = new Object();
    private static final Map<Module, State> STATES = new EnumMap<>(Module.class);
    private static GoncForegroundService instance;

    /** Map the shared UiKit connection state ("failed"/"connected"/…) to a dot. */
    static Dot dotForState(String connectionState) {
        if ("failed".equals(connectionState)) {
            return Dot.RED;
        }
        if ("connected".equals(connectionState)) {
            return Dot.GREEN;
        }
        return Dot.YELLOW;
    }

    /**
     * Replace the live set of active modules and their state. An empty map stops
     * the service. Safe to call from any thread: the service is started on the
     * first active module and stopped after the last one.
     */
    static void apply(Context context, Map<Module, State> active) {
        Context app = context.getApplicationContext();
        synchronized (LOCK) {
            // Callers refresh liberally (on every metric tick); skip no-op updates
            // so the notification isn't rebuilt unless something visible changed.
            if (STATES.equals(active)) {
                return;
            }
            STATES.clear();
            STATES.putAll(active);
            if (instance != null) {
                instance.onStateChanged();
            } else if (!STATES.isEmpty()) {
                Intent intent = new Intent(app, GoncForegroundService.class).setAction(ACTION_REFRESH);
                ContextCompat.startForegroundService(app, intent);
            }
        }
    }

    @Override
    public void onCreate() {
        super.onCreate();
        synchronized (LOCK) {
            instance = this;
        }
        createNotificationChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        // Always satisfy the "startForeground within 5s of startForegroundService"
        // contract before deciding whether there is anything to keep alive.
        startInForeground();
        boolean idle;
        synchronized (LOCK) {
            idle = STATES.isEmpty();
        }
        if (idle) {
            shutdown();
        }
        return START_NOT_STICKY;
    }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    @Override
    public void onDestroy() {
        synchronized (LOCK) {
            if (instance == this) {
                instance = null;
            }
        }
        super.onDestroy();
    }

    /** Called (holding {@link #LOCK}) when the active-module set changes while running. */
    private void onStateChanged() {
        if (STATES.isEmpty()) {
            shutdown();
        } else {
            NotificationManager manager = getSystemService(NotificationManager.class);
            if (manager != null) {
                manager.notify(NOTIFICATION_ID, buildNotification());
            }
        }
    }

    private void startInForeground() {
        Notification notification = buildNotification();
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                ServiceCompat.startForeground(this, NOTIFICATION_ID, notification,
                        ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
            } else {
                startForeground(NOTIFICATION_ID, notification);
            }
        } catch (RuntimeException ignored) {
            // e.g. a start/stop race; the matching stop will follow.
        }
    }

    private void shutdown() {
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                stopForeground(Service.STOP_FOREGROUND_REMOVE);
            } else {
                stopForeground(true);
            }
        } catch (RuntimeException ignored) {
        }
        stopSelf();
    }

    private Notification buildNotification() {
        List<String> lines = new ArrayList<>();
        int progress = -1;
        synchronized (LOCK) {
            State send = STATES.get(Module.SEND);
            State receive = STATES.get(Module.RECEIVE);
            State vpnServer = STATES.get(Module.VPN_SERVER);
            if (send != null) {
                lines.add(dot(send.dot) + getString(R.string.fg_sharing));
            }
            if (receive != null) {
                String label = receive.progress >= 0
                        ? getString(R.string.fg_receiving_progress, receive.progress)
                        : getString(R.string.fg_receiving);
                lines.add(dot(receive.dot) + label);
                if (receive.progress >= 0) {
                    progress = receive.progress;
                }
            }
            if (vpnServer != null) {
                lines.add(dot(vpnServer.dot) + getString(R.string.fg_vpn_server_running));
            }
        }
        if (lines.isEmpty()) {
            lines.add(getString(R.string.app_name));
        }

        Intent intent = new Intent(this, MainActivity.class);
        PendingIntent pendingIntent = PendingIntent.getActivity(
                this, 0, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        NotificationCompat.Builder builder = new NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle(getString(R.string.app_name))
                .setContentText(joinLines(lines))
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentIntent(pendingIntent)
                .setOngoing(true);

        if (lines.size() > 1) {
            NotificationCompat.InboxStyle style = new NotificationCompat.InboxStyle();
            for (String line : lines) {
                style.addLine(line);
            }
            builder.setStyle(style);
        }
        // Only the receive module reports a determinate value; show its bar when present.
        if (progress >= 0) {
            builder.setProgress(100, progress, false);
        }
        return builder.build();
    }

    private static String dot(Dot dot) {
        // Build the emoji from its code point so the .java source stays pure ASCII
        // (avoids any source-encoding ambiguity on the build machine).
        final int codePoint;
        switch (dot) {
            case GREEN:
                codePoint = 0x1F7E2; // large green circle
                break;
            case RED:
                codePoint = 0x1F534; // large red circle
                break;
            default:
                codePoint = 0x1F7E1; // large yellow circle
                break;
        }
        return new String(Character.toChars(codePoint)) + " ";
    }

    private static String joinLines(List<String> lines) {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < lines.size(); i++) {
            if (i > 0) {
                sb.append(" · ");
            }
            sb.append(lines.get(i));
        }
        return sb.toString();
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return;
        }
        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                getString(R.string.fg_notification_channel),
                NotificationManager.IMPORTANCE_LOW);
        NotificationManager manager = getSystemService(NotificationManager.class);
        if (manager != null) {
            manager.createNotificationChannel(channel);
        }
    }
}
