package cn.threatexpert.gonc;

import android.app.AlertDialog;
import android.content.Context;
import android.content.res.ColorStateList;
import android.content.res.Configuration;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.provider.Settings;
import android.text.InputFilter;
import android.text.InputType;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;

import androidx.core.view.ViewCompat;

import java.io.File;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Consumer;

import nearbygo.Nearbygo;
import nearbygo.NearbyListener;
import nearbygo.NearbyNode;

/**
 * Nearby on Android: send to and receive from Gonc devices on the same
 * network without a passphrase. It is not a separate screen. Send Files and
 * Receive Files each have a Passphrase / Nearby switch; in Nearby mode this
 * controller draws the device list (send) or the ready-to-receive controls
 * (receive) in place of the passphrase area.
 *
 * The Go service only announces this device while a Nearby view is on
 * screen and the app is in the foreground, holding a Wi-Fi multicast lock
 * for that time. Accepted transfers are downloaded by the Receive screen's
 * usual receiver from the local endpoint the Go side provides.
 */
final class NearbyController {
    /** Activity-level actions the controller needs. */
    interface Host {
        /** Show Receive Files, where an accepted transfer is downloaded. */
        void showReceiveForNearby();
    }

    static final String SCREEN_SEND = "send";
    static final String SCREEN_RECEIVE = "receive";
    private static final String PREF_SEND_MODE = "nearby_send_mode";
    private static final String PREF_RECEIVE_MODE = "nearby_receive_mode";
    private static final int DEVICE_NAME_MAX_LENGTH = 24;
    private static final int OFFER_ITEMS_SHOWN = 6;

    private ModuleHost host;
    private Host activity;
    private ReceiveController receive;

    private volatile NearbyNode node;
    private boolean starting;
    private String startError = "";
    private NearbyModel.State state = new NearbyModel.State();
    private final Map<String, NearbyModel.Transfer> transfers = new LinkedHashMap<>();
    private final Map<String, AndroidFileSource> sendSources = new HashMap<>();
    private final Set<String> busyDevices = new HashSet<>();
    /** Send progress shown in the Activity panel while Nearby mode is on. */
    private final TransferMetrics sendMetrics = new TransferMetrics();
    private final Set<String> announcedOffers = new HashSet<>();
    /**
     * Every device seen since Nearby started, so a row with a send in
     * progress stays on screen after its device leaves the list.
     */
    private final Map<String, NearbyModel.Device> lastKnown = new HashMap<>();
    /** Rows of fading devices; their look and "last seen" text update every second. */
    private final Map<String, View> fadingRows = new HashMap<>();
    private final Map<String, TextView> fadingStatus = new HashMap<>();
    private final Runnable vitalityTick = this::tickVitality;
    private boolean vitalityScheduled;
    private boolean awayPeersExpanded;

    private boolean foreground;
    private String screen = "";
    private boolean discovering;
    private WifiManager.MulticastLock multicastLock;

    /** Status text and progress bar of each send row, for in-place progress. */
    private final Map<String, TextView> sendStatusViews = new HashMap<>();
    private final Map<String, ProgressBar> sendProgressViews = new HashMap<>();

    private AlertDialog pairingDialog;
    private String pairingDialogId = "";
    private AlertDialog offerDialog;
    private String offerDialogId = "";

    NearbyController(ModuleHost host) {
        this.host = host;
    }

    void attach(ModuleHost host, Host activity, ReceiveController receive) {
        this.host = host;
        this.activity = activity;
        this.receive = receive;
        // Dialogs belong to the old activity after a configuration change.
        pairingDialog = null;
        offerDialog = null;
        pairingDialogId = "";
        offerDialogId = "";
        // Re-ask about a request the previous activity was showing.
        host.mainHandler().post(this::showNextOffer);
    }

    /** Dialogs need a live activity; skip them while it is going away. */
    private boolean canShowDialogs() {
        Context c = host.context();
        return !(c instanceof android.app.Activity) || !((android.app.Activity) c).isFinishing();
    }

    // --- mode and discovery ----------------------------------------------

    boolean isNearbySend() {
        return host.prefs().getBoolean(PREF_SEND_MODE, false);
    }

    boolean isNearbyReceive() {
        return host.prefs().getBoolean(PREF_RECEIVE_MODE, false);
    }

    void setNearbySend(boolean on) {
        host.prefs().edit().putBoolean(PREF_SEND_MODE, on).apply();
        if (!on) awayPeersExpanded = false;
        updateDiscovery();
        host.requestImmediateRender();
    }

    void setNearbyReceive(boolean on) {
        host.prefs().edit().putBoolean(PREF_RECEIVE_MODE, on).apply();
        updateDiscovery();
        host.requestImmediateRender();
    }

    /** Called from the activity's onResume/onPause. */
    void setForeground(boolean on) {
        foreground = on;
        updateDiscovery();
        if (on && discovering) {
            // Announce at once and join the network again if it changed
            // while the app was in the background.
            call(NearbyNode::refresh);
        }
    }

    /** Called whenever the visible module changes. */
    void setScreen(String module) {
        screen = module == null ? "" : module;
        if (!SCREEN_SEND.equals(screen)) awayPeersExpanded = false;
        updateDiscovery();
    }

    private boolean wantsDiscovery() {
        return foreground && ((SCREEN_SEND.equals(screen) && isNearbySend())
                || (SCREEN_RECEIVE.equals(screen) && isNearbyReceive()));
    }

    private void updateDiscovery() {
        boolean want = wantsDiscovery();
        if (want) {
            ensureStarted();
        }
        NearbyNode current = node;
        if (current == null || want == discovering) {
            return;
        }
        discovering = want;
        if (want) {
            acquireMulticast();
        }
        current.setDiscovering(want);
        if (!want) {
            releaseMulticast();
        }
    }

    private void acquireMulticast() {
        if (multicastLock != null) {
            return;
        }
        try {
            WifiManager wifi = (WifiManager) host.context().getApplicationContext().getSystemService(Context.WIFI_SERVICE);
            if (wifi == null) {
                return;
            }
            WifiManager.MulticastLock lock = wifi.createMulticastLock("gonc-nearby");
            lock.setReferenceCounted(false);
            lock.acquire();
            multicastLock = lock;
        } catch (RuntimeException error) {
            host.log("warn", "Nearby: Wi-Fi multicast unavailable: " + error.getMessage());
        }
    }

    private void releaseMulticast() {
        WifiManager.MulticastLock lock = multicastLock;
        multicastLock = null;
        if (lock != null && lock.isHeld()) {
            try {
                lock.release();
            } catch (RuntimeException ignored) {
                // Already released.
            }
        }
    }

    private void ensureStarted() {
        if (node != null || starting) {
            return;
        }
        starting = true;
        startError = "";
        Context context = host.context().getApplicationContext();
        String name = defaultDeviceName(context);
        String type = deviceType(context);
        File storeFile = new File(context.getNoBackupFilesDir(), "nearby.bin");
        Thread worker = new Thread(() -> {
            NearbyNode started = null;
            String error = "";
            try {
                NearbyKey.Loaded key = NearbyKey.load(host.prefs());
                if (key.created && storeFile.exists()) {
                    // A store sealed with a key that no longer exists cannot be
                    // opened; start over with a new identity.
                    //noinspection ResultOfMethodCallIgnored
                    storeFile.delete();
                }
                started = Nearbygo.startNearby(storeFile.getAbsolutePath(), key.key, name, type, listener);
            } catch (Throwable t) {
                error = t.getMessage() == null ? t.toString() : t.getMessage();
            }
            NearbyNode result = started;
            String failure = error;
            host.mainHandler().post(() -> {
                starting = false;
                if (result == null) {
                    startError = failure;
                    host.log("error", "Nearby: could not start: " + failure);
                    host.requestRender();
                    return;
                }
                node = result;
                setState(NearbyModel.State.parse(result.stateJSON()));
                discovering = false;
                updateDiscovery();
                host.requestRender();
            });
        }, "gonc-nearby-start");
        worker.start();
    }

    private static String defaultDeviceName(Context context) {
        String name = null;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N_MR1) {
            name = Settings.Global.getString(context.getContentResolver(), Settings.Global.DEVICE_NAME);
        }
        if (name == null || name.trim().isEmpty()) {
            name = Build.MODEL;
        }
        return name == null ? "Android" : name.trim();
    }

    private static String deviceType(Context context) {
        Configuration config = context.getResources().getConfiguration();
        return config.smallestScreenWidthDp >= 600 ? "tablet" : "phone";
    }

    /** Stops Nearby on a real exit. */
    void endTask() {
        NearbyNode current = node;
        node = null;
        discovering = false;
        releaseMulticast();
        for (AndroidFileSource source : sendSources.values()) {
            source.closeAll();
        }
        sendSources.clear();
        dismissDialogs();
        if (current != null) {
            new Thread(current::close, "gonc-nearby-close").start();
        }
    }

    private void dismissDialogs() {
        if (pairingDialog != null) pairingDialog.dismiss();
        if (offerDialog != null) offerDialog.dismiss();
        pairingDialog = null;
        offerDialog = null;
        pairingDialogId = "";
        offerDialogId = "";
    }

    /**
     * The Activity panel's metrics on the Send screen: Nearby's sends while
     * Nearby mode is on or a Nearby send is running, otherwise null.
     */
    TransferMetrics sendMetricsIfShown() {
        return isNearbySend() || hasActiveSend() ? sendMetrics : null;
    }

    /** True while a send is running, for the foreground service and exit prompt. */
    boolean hasActiveSend() {
        for (NearbyModel.Transfer t : transfers.values()) {
            if (t.isSend() && !t.finished()) return true;
        }
        return false;
    }

    /** Foreground-service contribution for running sends, or null. */
    GoncForegroundService.State foregroundState() {
        long done = 0;
        long total = 0;
        boolean any = false;
        for (NearbyModel.Transfer t : transfers.values()) {
            if (t.isSend() && !t.finished()) {
                any = true;
                done += t.doneBytes;
                total += t.totalBytes;
            }
        }
        if (!any) return null;
        int progress = total > 0 ? (int) Math.min(100, done * 100 / total) : -1;
        return new GoncForegroundService.State(GoncForegroundService.Dot.GREEN, progress);
    }

    // --- events from Go --------------------------------------------------

    private final NearbyListener listener = (kind, payload) -> {
        ModuleHost h = host;
        h.mainHandler().post(() -> handleEvent(kind, payload));
    };

    private void handleEvent(String kind, String payload) {
        if (node == null && !starting) {
            return; // stopped
        }
        switch (kind) {
            case "state":
                setState(NearbyModel.State.parse(payload));
                if (nearbyOnScreen()) {
                    host.requestRender();
                }
                break;
            case "log":
                host.log("info", "Nearby: " + payload);
                break;
            case "pairing":
                onPairing(NearbyModel.Pairing.parse(payload));
                break;
            case "transfer":
                onTransfer(NearbyModel.Transfer.parse(payload));
                break;
            default:
                break;
        }
    }

    private void onPairing(NearbyModel.Pairing p) {
        if ("waiting".equals(p.status)) {
            showPairingDialog(p);
            return;
        }
        if (p.id.equals(pairingDialogId) && pairingDialog != null) {
            pairingDialog.dismiss();
            pairingDialog = null;
            pairingDialogId = "";
        }
        if ("paired".equals(p.status)) {
            toast(host.context().getString(R.string.nearby_paired_with, p.peerName));
        } else if ("rejected".equals(p.status)) {
            toast(host.context().getString(R.string.nearby_pair_rejected));
        } else if ("failed".equals(p.status)) {
            toast(host.context().getString(R.string.nearby_pair_failed, p.error));
        }
        host.requestRender();
    }

    private void onTransfer(NearbyModel.Transfer t) {
        if (t.id.isEmpty()) {
            return;
        }
        transfers.put(t.id, t);
        if (t.isReceive()) {
            onReceiveTransfer(t);
        } else if (t.finished()) {
            onSendFinished(t);
        }
        if (t.isSend()) {
            NearbyModel.applySendMetrics(sendMetrics, transfers.values(), System.currentTimeMillis());
            host.requestBackgroundRender();
        }
        host.refreshForegroundService();
        if (!updateSendRowInPlace(t)) {
            host.requestRender();
        }
    }

    private void setState(NearbyModel.State next) {
        state = next;
        for (NearbyModel.Device d : next.devices) {
            lastKnown.put(d.id, d);
        }
    }

    private NearbyModel.Device listedDevice(String id) {
        for (NearbyModel.Device d : state.devices) {
            if (d.id.equals(id)) return d;
        }
        return null;
    }

    private void scheduleVitalityTick() {
        if (vitalityScheduled || fadingRows.isEmpty()) {
            return;
        }
        vitalityScheduled = true;
        host.mainHandler().postDelayed(vitalityTick, 1000);
    }

    /** Fades rows of silent devices and counts their "last seen" seconds in place. */
    private void tickVitality() {
        vitalityScheduled = false;
        if (!nearbyOnScreen()) {
            return;
        }
        long now = System.currentTimeMillis();
        Context c = host.context();
        for (Map.Entry<String, View> e : fadingRows.entrySet()) {
            NearbyModel.Device d = listedDevice(e.getKey());
            if (d == null || !d.fading()) continue;
            e.getValue().animate().alpha(NearbyModel.vitality(d, now, state.deviceFadeMs, state.deviceExpiryMs))
                    .setDuration(900).start();
            TextView status = fadingStatus.get(e.getKey());
            if (status != null) {
                status.setText(c.getString(R.string.nearby_last_seen, NearbyModel.secondsSilent(d, now)));
            }
        }
        scheduleVitalityTick();
    }

    /** A small dot before a device name: online, fading or away. */
    private View presenceDot(String presence) {
        UiKit u = host.ui();
        View dot = new View(host.context());
        GradientDrawable g = new GradientDrawable();
        g.setShape(GradientDrawable.OVAL);
        if ("online".equals(presence)) {
            g.setColor(Color.rgb(22, 163, 74));
        } else if ("fading".equals(presence)) {
            g.setColor(Color.TRANSPARENT);
            g.setStroke(Math.max(2, u.dp(1) * 3 / 2), Color.rgb(148, 163, 184));
        } else {
            g.setColor(Color.rgb(203, 213, 225));
        }
        dot.setBackground(g);
        return dot;
    }

    private static String presenceOf(NearbyModel.Device device) {
        if (device == null) return "away";
        return device.fading() ? "fading" : "online";
    }

    /** A device name with its presence dot in front. */
    private LinearLayout nameLine(String name, String presence, int size, int color, int style) {
        UiKit u = host.ui();
        LinearLayout line = u.row();
        line.setGravity(Gravity.CENTER_VERTICAL);
        LinearLayout.LayoutParams dotParams = new LinearLayout.LayoutParams(u.dp(8), u.dp(8));
        dotParams.setMarginEnd(u.dp(6));
        line.addView(presenceDot(presence), dotParams);
        TextView text = u.text(name, size, color, style);
        text.setSingleLine(true);
        text.setEllipsize(android.text.TextUtils.TruncateAt.END);
        line.addView(text, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        return line;
    }

    private void addDeviceIcon(LinearLayout row, String type, boolean highlighted, boolean compact) {
        UiKit u = host.ui();
        ImageView icon = new ImageView(host.context());
        boolean mobile = "phone".equals(type) || "tablet".equals(type);
        icon.setImageResource(mobile ? R.drawable.ic_nearby_phone : R.drawable.ic_nearby_monitor);
        icon.setColorFilter(highlighted ? Color.rgb(40, 112, 216) : u.muted());
        icon.setBackground(u.rounded(highlighted ? Color.rgb(232, 241, 255) : Color.rgb(241, 245, 249),
                u.dp(7), 0, 0));
        int inset = u.dp(compact ? 4 : 6);
        icon.setPadding(inset, inset, inset, inset);
        icon.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
        int size = u.dp(compact ? 24 : 32);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(size, size);
        params.setMarginEnd(u.dp(compact ? 6 : 8));
        row.addView(icon, 0, params);
    }

    /** The last row of the list: Nearby keeps looking after devices appear. */
    private View searchingMoreRow() {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(u.dp(12), u.dp(9), u.dp(10), u.dp(9));
        GradientDrawable border = new GradientDrawable();
        border.setCornerRadius(u.dp(7));
        border.setColor(Color.TRANSPARENT);
        border.setStroke(Math.max(1, u.dp(1)), Color.rgb(203, 213, 225), u.dp(4), u.dp(3));
        row.setBackground(border);
        LinearLayout.LayoutParams pulseParams = new LinearLayout.LayoutParams(u.dp(22), u.dp(22));
        pulseParams.setMarginEnd(u.dp(10));
        row.addView(new PulseView(c, Color.rgb(40, 112, 216)), pulseParams);
        LinearLayout labels = u.column();
        labels.addView(u.text(c.getString(R.string.nearby_searching_more), 13, u.muted(), Typeface.BOLD));
        TextView hint = u.text(c.getString(R.string.nearby_searching_more_hint), 12, u.muted(), Typeface.NORMAL);
        hint.setSingleLine(false);
        labels.addView(hint);
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        return row;
    }

    private boolean nearbyOnScreen() {
        return (SCREEN_SEND.equals(screen) && isNearbySend()) || (SCREEN_RECEIVE.equals(screen) && isNearbyReceive());
    }

    /** Progress of a running send updates its row without rebuilding the screen. */
    private boolean updateSendRowInPlace(NearbyModel.Transfer t) {
        if (!t.isSend() || !"transferring".equals(t.status)) {
            return false;
        }
        TextView status = sendStatusViews.get(t.id);
        ProgressBar bar = sendProgressViews.get(t.id);
        if (status == null || bar == null || !status.isAttachedToWindow()) {
            return !nearbyOnScreen();
        }
        status.setText(sendStatus(t));
        bar.setProgress(Math.max(0, t.percent()));
        return true;
    }

    private void onSendFinished(NearbyModel.Transfer t) {
        busyDevices.remove(t.deviceId);
        AndroidFileSource source = sendSources.remove(t.id);
        if (source != null) {
            source.closeAll();
        }
        Context c = host.context();
        switch (t.status) {
            case "completed":
                host.log("info", "Nearby: " + c.getString(R.string.nearby_sent, t.peerName));
                toast(c.getString(R.string.nearby_sent, t.peerName));
                break;
            case "declined":
                toast(c.getString(R.string.nearby_send_declined, t.peerName));
                break;
            case "cancelled":
                host.log("warn", "Nearby: " + c.getString(R.string.nearby_send_cancelled, t.peerName)
                        + (t.message.isEmpty() ? "" : " (" + t.message + ")"));
                break;
            default:
                String reason = sendFailureReason(t);
                String failure = c.getString(R.string.nearby_send_failed, t.peerName, reason);
                host.log("error", "Nearby: " + failure);
                if ("not-paired".equals(t.errorCode)) {
                    showNotPairedDialog(t);
                } else {
                    toast(failure);
                }
                break;
        }
    }

    /** The app's own wording for failures the Go side names with a code. */
    private String sendFailureReason(NearbyModel.Transfer t) {
        Context c = host.context();
        switch (t.errorCode) {
            case "not-paired":
                return c.getString(R.string.nearby_error_not_paired);
            case "quick-refused":
                return c.getString(R.string.nearby_error_quick_refused);
            case "busy":
                return c.getString(R.string.nearby_error_busy);
            case "timeout":
                return c.getString(R.string.nearby_error_timeout);
            default:
                return t.message;
        }
    }

    /**
     * The other device forgot the pairing. Offer to forget it here too, so
     * the device shows Pair again.
     */
    private void showNotPairedDialog(NearbyModel.Transfer t) {
        if (!canShowDialogs()) {
            toast(host.context().getString(R.string.nearby_send_failed, t.peerName, sendFailureReason(t)));
            return;
        }
        Context c = host.context();
        String peerId = peerIdFor(t.deviceId);
        AlertDialog.Builder b = new AlertDialog.Builder(c)
                .setTitle(c.getString(R.string.nearby_not_paired_title, t.peerName))
                .setMessage(c.getString(R.string.nearby_not_paired_body, t.peerName));
        if (peerId.isEmpty()) {
            b.setPositiveButton(android.R.string.ok, null);
        } else {
            b.setNegativeButton(R.string.cancel, null)
                    .setPositiveButton(R.string.nearby_forget_pairing, (d, w) -> call(n -> n.forget(peerId)));
        }
        b.show();
    }

    /** The saved pairing behind a device ID, or "". */
    private String peerIdFor(String deviceId) {
        for (NearbyModel.Device d : state.devices) {
            if (d.id.equals(deviceId) && d.paired) return d.peerId;
        }
        return deviceId.startsWith("p-") ? deviceId.substring(2) : "";
    }

    private void onReceiveTransfer(NearbyModel.Transfer t) {
        switch (t.status) {
            case "offered":
                if (announcedOffers.add(t.id)) {
                    showOfferDialog(t);
                }
                return;
            case "ready":
                if (receive != null && receive.beginNearbyReceive(t.id, t.peerName, t.endpoint)) {
                    if (activity != null) activity.showReceiveForNearby();
                } else {
                    cancel(t.id);
                }
                return;
            default:
                break;
        }
        if (!t.finished()) {
            return;
        }
        announcedOffers.remove(t.id);
        if (t.id.equals(offerDialogId) && offerDialog != null) {
            offerDialog.dismiss();
            offerDialog = null;
            offerDialogId = "";
        }
        if (receive != null) {
            receive.onNearbyReceiveEnded(t.id);
        }
        Context c = host.context();
        if ("cancelled".equals(t.status) && !t.message.isEmpty()) {
            toast(c.getString(R.string.nearby_receive_cancelled, t.peerName));
        } else if ("failed".equals(t.status)) {
            String failure = c.getString(R.string.nearby_receive_failed, t.peerName, t.message);
            host.log("error", "Nearby: " + failure);
            toast(failure);
        }
    }

    // --- actions ---------------------------------------------------------

    private interface NodeAction {
        void run(NearbyNode node) throws Exception;
    }

    /** Runs a call into Go off the main thread and toasts its error. */
    private void call(NodeAction action) {
        NearbyNode current = node;
        if (current == null) {
            return;
        }
        new Thread(() -> {
            try {
                action.run(current);
            } catch (Exception error) {
                String message = error.getMessage() == null ? error.toString() : error.getMessage();
                host.mainHandler().post(() -> toast(message));
            }
        }, "gonc-nearby-call").start();
    }

    void cancel(String id) {
        call(n -> n.cancel(id));
    }

    /** Called by ReceiveController when its download of an accepted transfer ends. */
    void finishReceive(String id, boolean ok, String message) {
        NearbyNode current = node;
        if (current != null) {
            new Thread(() -> current.finishReceive(id, ok, message == null ? "" : message), "gonc-nearby-finish").start();
        }
    }

    private void sendTo(NearbyModel.Device device, List<ShareItem> items, boolean quick) {
        if (items.isEmpty()) {
            host.toast(R.string.nearby_add_first);
            return;
        }
        NearbyNode current = node;
        if (current == null) {
            return;
        }
        List<ShareItem> snapshot = new ArrayList<>(items);
        busyDevices.add(device.id);
        host.requestRender();
        Context context = host.context().getApplicationContext();
        new Thread(() -> {
            AndroidFileSource source = new AndroidFileSource(context, snapshot);
            try {
                String id = current.send(device.id, source, quick);
                host.mainHandler().post(() -> {
                    NearbyModel.Transfer t = transfers.get(id);
                    if (t != null && t.finished()) {
                        source.closeAll();
                    } else {
                        sendSources.put(id, source);
                    }
                    busyDevices.remove(device.id);
                    host.requestRender();
                });
            } catch (Exception error) {
                source.closeAll();
                String message = error.getMessage() == null ? error.toString() : error.getMessage();
                host.mainHandler().post(() -> {
                    busyDevices.remove(device.id);
                    toast(message);
                    host.requestRender();
                });
            }
        }, "gonc-nearby-send").start();
    }

    private void pair(NearbyModel.Device device) {
        busyDevices.add(device.id);
        host.requestRender();
        NearbyNode current = node;
        if (current == null) {
            return;
        }
        new Thread(() -> {
            String failure = null;
            try {
                current.pair(device.id);
            } catch (Exception error) {
                failure = error.getMessage() == null ? error.toString() : error.getMessage();
            }
            String message = failure;
            host.mainHandler().post(() -> {
                busyDevices.remove(device.id);
                if (message != null) toast(message);
                host.requestRender();
            });
        }, "gonc-nearby-pair").start();
    }

    private NearbyModel.Transfer activeSendTo(String deviceId) {
        NearbyModel.Transfer found = null;
        for (NearbyModel.Transfer t : transfers.values()) {
            if (t.isSend() && !t.finished() && deviceId.equals(t.deviceId)) {
                found = t;
            }
        }
        return found;
    }

    private void toast(String message) {
        android.widget.Toast.makeText(host.context(), message, android.widget.Toast.LENGTH_SHORT).show();
    }

    // --- views -----------------------------------------------------------

    /** The transfer method selector shared by Send and Receive. */
    View modeSwitch(boolean nearby, boolean enabled, Consumer<Boolean> onChange) {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout box = u.column();
        box.addView(u.sectionBoundaryTitle(c.getString(R.string.transfer_method), false));

        LinearLayout options = u.row();
        options.setPadding(u.dp(3), u.dp(2), u.dp(3), u.dp(2));
        options.setBackground(u.rounded(Color.rgb(241, 245, 249), u.dp(11), Color.rgb(226, 232, 240), 1));
        Button passphrase = transferMethodButton(c.getString(R.string.nearby_mode_passphrase), !nearby);
        passphrase.setOnClickListener(v -> {
            if (nearby) onChange.accept(false);
        });
        Button near = transferMethodButton(c.getString(R.string.nearby_mode_nearby), nearby);
        near.setOnClickListener(v -> {
            if (!nearby) onChange.accept(true);
        });
        u.setControlEnabled(passphrase, enabled);
        u.setControlEnabled(near, enabled);
        LinearLayout.LayoutParams passphraseParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.MATCH_PARENT, 1);
        passphraseParams.setMarginEnd(u.dp(4));
        options.addView(passphrase, passphraseParams);
        options.addView(near, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.MATCH_PARENT, 1));
        box.addView(options, u.blockParams(u.dp(6)));
        return box;
    }

    private Button transferMethodButton(String label, boolean selected) {
        UiKit u = host.ui();
        int fill = selected ? Color.WHITE : Color.TRANSPARENT;
        int textColor = selected ? Color.rgb(31, 91, 175) : u.muted();
        Button button = u.button(selected ? "✓  " + label : label, textColor, fill);
        button.setContentDescription(label);
        button.setSelected(selected);
        button.setTextSize(13);
        button.setMinWidth(0);
        button.setMinimumWidth(0);
        button.setMinHeight(u.dp(36));
        button.setMinimumHeight(u.dp(36));
        button.setPadding(u.dp(8), u.dp(5), u.dp(8), u.dp(5));
        button.setBackground(new RippleDrawable(ColorStateList.valueOf(Color.rgb(219, 231, 249)),
                u.rounded(fill, u.dp(8), Color.rgb(190, 210, 240), selected ? 1 : 0),
                u.rounded(Color.WHITE, u.dp(8), 0, 0)));
        return button;
    }

    /** Device list shown on Send Files in Nearby mode. */
    View sendSection(List<ShareItem> items) {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout box = u.column();
        box.addView(u.text(c.getString(R.string.nearby_send_method_hint), 12, u.muted(), Typeface.NORMAL),
                u.blockParams(u.dp(8)));
        if (node == null) {
            box.addView(statusLine(startError.isEmpty() ? c.getString(R.string.nearby_starting)
                    : c.getString(R.string.nearby_start_failed, startError)), u.blockParams(u.dp(10)));
            return box;
        }
        if (!state.discoveryError.isEmpty()) {
            box.addView(warning(c.getString(R.string.nearby_discovery_unavailable)), u.blockParams(u.dp(10)));
        }
        sendStatusViews.clear();
        sendProgressViews.clear();
        fadingRows.clear();
        fadingStatus.clear();
        // Rows with a send or a connection attempt stay after their device
        // leaves the list.
        Set<String> listed = new HashSet<>();
        for (NearbyModel.Device d : state.devices) listed.add(d.id);
        Set<String> pinned = new HashSet<>(busyDevices);
        for (NearbyModel.Transfer t : transfers.values()) {
            if (t.isSend() && !t.finished() && !t.deviceId.isEmpty()) pinned.add(t.deviceId);
        }
        List<NearbyModel.Device> away = new ArrayList<>();
        for (String id : pinned) {
            NearbyModel.Device d = lastKnown.get(id);
            if (!listed.contains(id) && d != null) away.add(d);
        }
        if (state.devices.isEmpty() && away.isEmpty()) {
            LinearLayout empty = u.column();
            empty.setGravity(Gravity.CENTER_HORIZONTAL);
            empty.setPadding(u.dp(12), u.dp(18), u.dp(12), u.dp(14));
            ProgressBar spinner = new ProgressBar(c);
            spinner.setIndeterminate(true);
            empty.addView(spinner, new LinearLayout.LayoutParams(u.dp(28), u.dp(28)));
            TextView title = u.text(c.getString(R.string.nearby_searching), 14, u.ink(), Typeface.BOLD);
            title.setGravity(Gravity.CENTER);
            empty.addView(title, u.blockParams(u.dp(8)));
            TextView hint = u.text(c.getString(R.string.nearby_searching_hint), 12, u.muted(), Typeface.NORMAL);
            hint.setGravity(Gravity.CENTER);
            empty.addView(hint, u.blockParams(u.dp(4)));
            box.addView(empty, u.blockParams(u.dp(6)));
        } else {
            for (NearbyModel.Device device : state.devices) {
                box.addView(deviceRow(device, items, false), u.blockParams(u.dp(8)));
            }
            for (NearbyModel.Device device : away) {
                box.addView(deviceRow(device, items, true), u.blockParams(u.dp(8)));
            }
            if (state.discovering) {
                box.addView(searchingMoreRow(), u.blockParams(u.dp(8)));
            }
        }
        List<NearbyModel.Peer> awayPeers = new ArrayList<>();
        for (NearbyModel.Peer peer : NearbyModel.byLastSeen(state.peers)) {
            String id = "p-" + peer.peerId;
            if (!listed.contains(id) && !pinned.contains(id) && peerDevice(peer.peerId) == null) awayPeers.add(peer);
        }
        box.addView(fallbackRow(), u.blockParams(u.dp(12)));
        if (awayPeers.isEmpty()) {
            awayPeersExpanded = false;
        } else {
            box.addView(awayPeersSection(awayPeers), u.blockParams(u.dp(4)));
        }
        scheduleVitalityTick();
        return box;
    }

    private View awayPeersSection(List<NearbyModel.Peer> peers) {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout section = u.column();
        LinearLayout header = u.row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        header.setMinimumHeight(u.dp(48));
        header.setPadding(u.dp(8), u.dp(4), u.dp(8), u.dp(4));
        header.setBackground(new RippleDrawable(ColorStateList.valueOf(Color.rgb(219, 231, 249)),
                null, u.rounded(Color.WHITE, u.dp(8), 0, 0)));
        header.setFocusable(true);

        String title = c.getString(R.string.nearby_paired_away);
        String count = c.getString(R.string.nearby_paired_away_count, peers.size());
        TextView titleView = u.text(title, 12, u.muted(), Typeface.BOLD);
        titleView.setGravity(Gravity.END);
        header.addView(titleView,
                new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        LinearLayout.LayoutParams countParams = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        countParams.setMarginStart(u.dp(4));
        header.addView(u.text(count, 12, u.muted(), Typeface.BOLD), countParams);
        ImageView chevron = new ImageView(c);
        chevron.setImageResource(R.drawable.ic_chevron_right);
        chevron.setColorFilter(u.muted());
        header.addView(chevron, new LinearLayout.LayoutParams(u.dp(18), u.dp(18)));
        header.setContentDescription(title + " " + count);
        ViewCompat.setScreenReaderFocusable(header, true);
        section.addView(header, u.blockParams(0));

        LinearLayout list = u.column();
        for (NearbyModel.Peer peer : peers) {
            LinearLayout row = nameLine(peer.nickname.isEmpty() ? peer.peerId : peer.nickname, "away", 13, u.muted(), Typeface.NORMAL);
            row.setPadding(u.dp(4), u.dp(6), u.dp(4), u.dp(2));
            addDeviceIcon(row, peer.deviceType, false, true);
            row.addView(u.text(lastSeenText(peer), 12, Color.rgb(148, 163, 184), Typeface.NORMAL));
            list.addView(row);
        }
        section.addView(list, u.blockParams(0));

        Runnable updateExpansion = () -> {
            list.setVisibility(awayPeersExpanded ? View.VISIBLE : View.GONE);
            chevron.setRotation(awayPeersExpanded ? 90f : 0f);
            ViewCompat.setStateDescription(header, c.getString(awayPeersExpanded
                    ? R.string.nearby_paired_away_expanded : R.string.nearby_paired_away_collapsed));
        };
        updateExpansion.run();
        header.setOnClickListener(v -> {
            awayPeersExpanded = !awayPeersExpanded;
            updateExpansion.run();
        });
        return section;
    }

    /** "Last seen 3 days ago" for a paired device that is not nearby. */
    private String lastSeenText(NearbyModel.Peer peer) {
        Context c = host.context();
        if (peer.lastSeenMs <= 0) {
            return c.getString(R.string.nearby_never_seen);
        }
        long now = System.currentTimeMillis();
        CharSequence ago = android.text.format.DateUtils.getRelativeTimeSpanString(
                Math.min(peer.lastSeenMs, now), now, android.text.format.DateUtils.MINUTE_IN_MILLIS);
        return c.getString(R.string.nearby_last_seen_ago, ago);
    }

    /** The listed device behind a saved pairing, or null when it is not nearby. */
    private NearbyModel.Device peerDevice(String peerId) {
        for (NearbyModel.Device d : state.devices) {
            if (d.paired && peerId.equals(d.peerId)) return d;
        }
        return null;
    }

    private View deviceRow(NearbyModel.Device device, List<ShareItem> items, boolean away) {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(u.dp(12), u.dp(9), u.dp(10), u.dp(9));
        row.setBackground(u.rounded(Color.rgb(251, 253, 255), u.dp(7), Color.rgb(226, 232, 240), 1));
        addDeviceIcon(row, device.deviceType, device.paired && !away, false);

        LinearLayout labels = u.column();
        String presence = away ? "away" : presenceOf(device);
        labels.addView(nameLine(device.displayName(), presence, 14, Color.rgb(38, 56, 79), Typeface.BOLD));
        NearbyModel.Transfer sending = activeSendTo(device.id);
        boolean busy = busyDevices.contains(device.id);
        if (sending != null) {
            TextView status = u.text(sendStatus(sending), 12, Color.rgb(40, 112, 216), Typeface.BOLD);
            labels.addView(status);
            if ("transferring".equals(sending.status)) {
                ProgressBar bar = new ProgressBar(c, null, android.R.attr.progressBarStyleHorizontal);
                bar.setMax(100);
                bar.setProgress(Math.max(0, sending.percent()));
                labels.addView(bar, u.blockParams(u.dp(2)));
                sendStatusViews.put(sending.id, status);
                sendProgressViews.put(sending.id, bar);
            }
        } else if (away) {
            labels.addView(u.text(c.getString(R.string.nearby_not_nearby), 12, u.muted(), Typeface.NORMAL));
        } else if (device.fading()) {
            long now = System.currentTimeMillis();
            TextView status = u.text(c.getString(R.string.nearby_last_seen, NearbyModel.secondsSilent(device, now)), 12,
                    u.muted(), Typeface.NORMAL);
            labels.addView(status);
            if (!busy) {
                // A row the user is working with stays fully visible.
                row.setAlpha(NearbyModel.vitality(device, now, state.deviceFadeMs, state.deviceExpiryMs));
                fadingRows.put(device.id, row);
                fadingStatus.put(device.id, status);
            }
        } else {
            TextView status = u.text(c.getString(device.paired ? R.string.nearby_paired : R.string.nearby_not_paired), 12,
                    device.paired ? Color.rgb(15, 111, 83) : u.muted(), Typeface.NORMAL);
            labels.addView(status);
        }
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

        if (away) {
            GradientDrawable dashed = new GradientDrawable();
            dashed.setCornerRadius(u.dp(7));
            dashed.setColor(Color.rgb(251, 253, 255));
            dashed.setStroke(Math.max(1, u.dp(1)), Color.rgb(203, 213, 225), u.dp(4), u.dp(3));
            row.setBackground(dashed);
        }
        if (sending != null) {
            Button cancel = u.compactGhostButton(c.getString(R.string.cancel));
            cancel.setOnClickListener(v -> cancel(sending.id));
            row.addView(cancel, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(36)));
        } else if (away) {
            // Not nearby: nothing to start until it comes back.
        } else if (busy) {
            Button wait = rowButton(u.secondaryButton(c.getString(R.string.nearby_connecting)));
            u.setControlEnabled(wait, false);
            row.addView(wait, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(ROW_BUTTON_DP)));
        } else {
            if (!device.paired) {
                Button pair = rowButton(u.secondaryButton(c.getString(R.string.nearby_pair)));
                pair.setOnClickListener(v -> pair(device));
                LinearLayout.LayoutParams pairParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(ROW_BUTTON_DP));
                pairParams.setMargins(u.dp(6), 0, 0, 0);
                row.addView(pair, pairParams);
            }
            if (device.paired || device.quickReceive) {
                Button send = rowButton(u.primaryButton(c.getString(R.string.nearby_send)));
                boolean quick = !device.paired;
                send.setOnClickListener(v -> sendTo(device, items, quick));
                LinearLayout.LayoutParams sendParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(ROW_BUTTON_DP));
                sendParams.setMargins(u.dp(6), 0, 0, 0);
                row.addView(send, sendParams);
            }
        }
        return row;
    }

    /** Height of the action buttons in device rows. */
    private static final int ROW_BUTTON_DP = 38;

    /**
     * Fits a button into a device row: the platform's default padding and
     * minimum height clip the label at row height, most visibly for Chinese.
     */
    private Button rowButton(Button button) {
        UiKit u = host.ui();
        button.setTextSize(14);
        button.setMinWidth(0);
        button.setMinimumWidth(u.dp(64));
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        button.setPadding(u.dp(14), 0, u.dp(14), 0);
        button.setIncludeFontPadding(false);
        return button;
    }

    private String sendStatus(NearbyModel.Transfer t) {
        Context c = host.context();
        switch (t.status) {
            case "waiting":
                return c.getString(R.string.nearby_status_waiting);
            case "transferring":
                int percent = t.percent();
                return percent >= 0 ? c.getString(R.string.nearby_status_sending, percent)
                        : c.getString(R.string.nearby_status_sending_unknown);
            default:
                return c.getString(R.string.nearby_status_connecting);
        }
    }

    private View fallbackRow() {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout box = u.column();
        box.addView(u.text(c.getString(R.string.nearby_not_listed), 12, u.muted(), Typeface.NORMAL));
        return box;
    }

    /** Ready-to-receive controls shown on Receive Files in Nearby mode. */
    View receiveSection() {
        UiKit u = host.ui();
        Context c = host.context();
        LinearLayout box = u.column();
        if (node == null) {
            box.addView(statusLine(startError.isEmpty() ? c.getString(R.string.nearby_starting)
                    : c.getString(R.string.nearby_start_failed, startError)), u.blockParams(u.dp(10)));
            return box;
        }
        if (!state.discoveryError.isEmpty()) {
            box.addView(warning(c.getString(R.string.nearby_discovery_unavailable)), u.blockParams(u.dp(10)));
        }

        LinearLayout banner = u.row();
        banner.setGravity(Gravity.CENTER_VERTICAL);
        banner.setPadding(u.dp(12), u.dp(10), u.dp(10), u.dp(10));
        boolean visible = state.visible;
        banner.setBackground(u.rounded(visible ? Color.rgb(230, 246, 238) : Color.rgb(248, 250, 252), u.dp(8),
                visible ? Color.rgb(171, 222, 197) : Color.rgb(226, 232, 240), 1));
        if (visible && state.discovering) {
            // This device is announcing itself: show it is on the air.
            LinearLayout.LayoutParams pulseParams = new LinearLayout.LayoutParams(u.dp(28), u.dp(28));
            pulseParams.setMarginEnd(u.dp(10));
            PulseView pulse = new PulseView(c, Color.rgb(15, 111, 83));
            pulse.setContentDescription(c.getString(R.string.nearby_broadcasting));
            banner.addView(pulse, pulseParams);
        }
        LinearLayout labels = u.column();
        int titleColor = visible ? Color.rgb(15, 111, 83) : u.ink();
        TextView title = u.text(visible ? c.getString(R.string.nearby_ready_title, state.nickname)
                : c.getString(R.string.nearby_hidden_title), 14, titleColor, Typeface.BOLD);
        title.setSingleLine(false);
        labels.addView(title);
        TextView body = u.text(c.getString(visible ? R.string.nearby_ready_body : R.string.nearby_hidden_body), 12,
                visible ? Color.rgb(15, 111, 83) : u.muted(), Typeface.NORMAL);
        labels.addView(body, u.blockParams(u.dp(2)));
        banner.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button action = visible ? u.compactGhostButton(c.getString(R.string.nearby_rename))
                : rowButton(u.secondaryButton(c.getString(R.string.nearby_show_device)));
        action.setOnClickListener(v -> {
            if (visible) {
                showRenameDialog();
            } else {
                call(n -> n.setVisible(true));
            }
        });
        LinearLayout.LayoutParams actionParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(36));
        actionParams.setMargins(u.dp(8), 0, 0, 0);
        banner.addView(action, actionParams);
        box.addView(banner, u.blockParams(u.dp(10)));

        CheckBox quick = new CheckBox(c);
        quick.setText(c.getString(R.string.nearby_accept_unpaired));
        quick.setTextColor(Color.rgb(64, 81, 105));
        quick.setTextSize(14);
        quick.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        quick.setChecked(state.quickReceive);
        quick.setOnCheckedChangeListener((buttonView, checked) -> call(n -> n.setQuickReceive(checked)));
        box.addView(quick, u.blockParams(u.dp(8)));
        TextView quickHint = u.text(c.getString(R.string.nearby_accept_unpaired_hint), 12, u.muted(), Typeface.NORMAL);
        quickHint.setPadding(u.dp(4), 0, 0, 0);
        box.addView(quickHint);

        if (!state.peers.isEmpty()) {
            box.addView(u.text(c.getString(R.string.nearby_paired_devices), 13, Color.rgb(64, 81, 105), Typeface.BOLD),
                    u.blockParams(u.dp(12)));
            for (NearbyModel.Peer peer : state.peers) {
                LinearLayout row = u.row();
                row.setGravity(Gravity.CENTER_VERTICAL);
                NearbyModel.Device nearbyNow = peerDevice(peer.peerId);
                addDeviceIcon(row, nearbyNow == null ? peer.deviceType : nearbyNow.deviceType, nearbyNow != null, true);
                LinearLayout name = nameLine(peer.nickname.isEmpty() ? peer.peerId : peer.nickname, presenceOf(nearbyNow),
                        14, u.ink(), Typeface.NORMAL);
                if (nearbyNow == null) {
                    name.addView(u.text(lastSeenText(peer), 12, Color.rgb(148, 163, 184), Typeface.NORMAL));
                }
                row.addView(name, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
                Button forget = u.compactGhostButton(c.getString(R.string.nearby_forget));
                forget.setTextColor(Color.rgb(138, 46, 46));
                forget.setOnClickListener(v -> confirmForget(peer));
                row.addView(forget, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(36)));
                box.addView(row, u.blockParams(u.dp(4)));
            }
        }
        return box;
    }

    private TextView statusLine(String message) {
        UiKit u = host.ui();
        TextView line = u.text(message, 13, u.muted(), Typeface.BOLD);
        line.setSingleLine(false);
        return line;
    }

    private TextView warning(String message) {
        UiKit u = host.ui();
        TextView line = u.text(message, 12, Color.rgb(133, 79, 11), Typeface.NORMAL);
        line.setSingleLine(false);
        line.setPadding(u.dp(10), u.dp(8), u.dp(10), u.dp(8));
        line.setBackground(u.rounded(Color.rgb(250, 238, 218), u.dp(6), 0, 0));
        return line;
    }

    // --- dialogs ---------------------------------------------------------

    private void showPairingDialog(NearbyModel.Pairing p) {
        Context c = host.context();
        if (!canShowDialogs()) {
            return;
        }
        if (pairingDialog != null) {
            pairingDialog.dismiss();
        }
        UiKit u = host.ui();
        LinearLayout content = u.column();
        content.setPadding(u.dp(24), u.dp(8), u.dp(24), u.dp(4));
        TextView code = u.text(p.formattedCode(), 34, u.ink(), Typeface.BOLD);
        code.setGravity(Gravity.CENTER);
        code.setLetterSpacing(0.08f);
        content.addView(code, u.blockParams(u.dp(4)));
        TextView body = u.text(c.getString(R.string.nearby_pair_body, p.peerName), 14, u.muted(), Typeface.NORMAL);
        body.setSingleLine(false);
        content.addView(body, u.blockParams(u.dp(12)));
        AlertDialog dialog = new AlertDialog.Builder(c)
                .setTitle(p.incoming ? c.getString(R.string.nearby_pair_title_in, p.peerName)
                        : c.getString(R.string.nearby_pair_title_out, p.peerName))
                .setView(content)
                .setCancelable(false)
                .setNegativeButton(R.string.cancel, (d, w) -> call(n -> n.confirmPairing(p.id, false)))
                .setPositiveButton(R.string.nearby_numbers_match, (d, w) -> call(n -> n.confirmPairing(p.id, true)))
                .create();
        dialog.setOnDismissListener(d -> {
            if (pairingDialog == dialog) {
                pairingDialog = null;
                pairingDialogId = "";
            }
        });
        pairingDialog = dialog;
        pairingDialogId = p.id;
        dialog.show();
    }

    private void showOfferDialog(NearbyModel.Transfer t) {
        Context c = host.context();
        UiKit u = host.ui();
        if (offerDialog != null || !canShowDialogs()) {
            // One request at a time: a second sender waits for the first answer.
            return;
        }
        LinearLayout content = u.column();
        content.setPadding(u.dp(24), u.dp(8), u.dp(24), u.dp(4));
        if (t.quick) {
            TextView unpaired = warning(c.getString(R.string.nearby_offer_unpaired, t.peerName));
            content.addView(unpaired, u.blockParams(0));
        }
        content.addView(u.text(c.getString(R.string.nearby_offer_summary, (int) t.fileCount, u.formatBytes(t.totalBytes)),
                14, u.ink(), Typeface.BOLD), u.blockParams(u.dp(10)));
        int shown = Math.min(OFFER_ITEMS_SHOWN, t.items.size());
        for (int i = 0; i < shown; i++) {
            NearbyModel.Item item = t.items.get(i);
            String line = (item.isDir ? "▸ " : "• ") + item.name + "  " + u.formatBytes(item.size);
            TextView text = u.text(line, 13, u.muted(), Typeface.NORMAL);
            text.setSingleLine(true);
            content.addView(text, u.blockParams(u.dp(3)));
        }
        if (t.items.size() > shown) {
            content.addView(u.text(c.getString(R.string.nearby_offer_more, t.items.size() - shown), 13, u.muted(),
                    Typeface.NORMAL), u.blockParams(u.dp(3)));
        }
        String saveTo = receive == null ? "" : receive.saveLocationLabel();
        if (!saveTo.isEmpty()) {
            TextView save = u.text(c.getString(R.string.nearby_offer_saving_to, saveTo), 12, u.muted(), Typeface.NORMAL);
            save.setSingleLine(false);
            content.addView(save, u.blockParams(u.dp(10)));
        }
        AlertDialog dialog = new AlertDialog.Builder(c)
                .setTitle(c.getString(R.string.nearby_offer_title, t.peerName))
                .setView(content)
                .setCancelable(false)
                .setNegativeButton(R.string.nearby_decline, null)
                .setPositiveButton(R.string.nearby_accept, null)
                .create();
        dialog.setOnShowListener(d -> {
            dialog.getButton(AlertDialog.BUTTON_NEGATIVE).setOnClickListener(v -> {
                call(n -> n.decline(t.id));
                dialog.dismiss();
            });
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v -> {
                if (receive != null && !receive.canAcceptNearby()) {
                    toast(c.getString(R.string.nearby_receive_busy));
                    return;
                }
                call(n -> n.accept(t.id));
                dialog.dismiss();
            });
        });
        dialog.setOnDismissListener(d -> {
            if (offerDialog == dialog) {
                offerDialog = null;
                offerDialogId = "";
                showNextOffer();
            }
        });
        offerDialog = dialog;
        offerDialogId = t.id;
        dialog.show();
    }

    private void showNextOffer() {
        for (NearbyModel.Transfer t : transfers.values()) {
            if (t.isReceive() && "offered".equals(t.status) && !t.id.equals(offerDialogId)) {
                showOfferDialog(t);
                return;
            }
        }
    }

    private void showRenameDialog() {
        Context c = host.context();
        UiKit u = host.ui();
        EditText input = new EditText(c);
        input.setSingleLine(true);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_CAP_WORDS);
        input.setFilters(new InputFilter[]{new InputFilter.LengthFilter(DEVICE_NAME_MAX_LENGTH)});
        input.setText(state.nickname);
        input.setSelectAllOnFocus(true);
        LinearLayout content = u.column();
        content.setPadding(u.dp(20), u.dp(8), u.dp(20), 0);
        content.addView(input);
        new AlertDialog.Builder(c)
                .setTitle(c.getString(R.string.nearby_rename_title, DEVICE_NAME_MAX_LENGTH))
                .setView(content)
                .setNegativeButton(R.string.cancel, null)
                .setPositiveButton(R.string.nearby_rename, (d, w) -> {
                    String name = input.getText().toString().trim();
                    if (!name.isEmpty()) {
                        call(n -> n.setNickname(name));
                    }
                })
                .show();
    }

    private void confirmForget(NearbyModel.Peer peer) {
        Context c = host.context();
        String name = peer.nickname.isEmpty() ? peer.peerId : peer.nickname;
        new AlertDialog.Builder(c)
                .setTitle(c.getString(R.string.nearby_forget_title, name))
                .setMessage(R.string.nearby_forget_body)
                .setNegativeButton(R.string.cancel, null)
                .setPositiveButton(R.string.nearby_forget, (d, w) -> call(n -> n.forget(peer.peerId)))
                .show();
    }
}
