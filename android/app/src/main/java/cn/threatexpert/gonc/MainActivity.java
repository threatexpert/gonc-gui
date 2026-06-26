package cn.threatexpert.gonc;

import android.app.Activity;
import android.app.AlertDialog;
import android.app.Dialog;
import android.Manifest;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.ContentResolver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.provider.DocumentsContract;
import android.provider.OpenableColumns;
import android.provider.Settings;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.ImageButton;
import android.widget.LinearLayout;
import android.widget.PopupMenu;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.ArrayAdapter;
import android.widget.AdapterView;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;

import androidx.core.graphics.Insets;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowInsetsCompat;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

import com.google.zxing.client.android.Intents;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

public final class MainActivity extends Activity implements ModuleHost {
    private static final String GONC_URL = "https://github.com/threatexpert/gonc";
    private static final String SOURCE_URL = "https://github.com/threatexpert/gonc-gui";
    private static final int REQUEST_OPEN_DOCUMENT = 1001;
    private static final int REQUEST_SCAN_QR = 1002;
    private static final int REQUEST_OPEN_SAVE_TREE = 1003;
    private static final int REQUEST_CAMERA_PERMISSION = 1004;
    private static final int REQUEST_OPEN_SEND_TREE = 1005;
    private static final int REQUEST_STORAGE_PERMISSION = 1006;
    private static final int REQUEST_VPN_PERMISSION = 1007;
    private static final int REQUEST_NOTIFICATION_PERMISSION = 1008;
    private boolean notificationPermissionRequested;
    private static final int MAX_ACTIVITY_LOGS = 500;
    private static final int MAX_VISIBLE_ACTIVITY_LOGS = 80;
    /** Coalescing window for high-frequency background (log/metric) re-renders. */
    private static final long BACKGROUND_RENDER_INTERVAL_MS = 200;
    private static final String PREFS = "gonc_main";
    private static final String KEY_VPN_PROFILES = "vpn_profiles";
    private static final String KEY_SELECTED_VPN_PROFILE = "selected_vpn_profile";
    private static final String VPN_PROFILE_QR_TYPE = "gonc.vpn.profile";

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final UiKit ui = new UiKit(this);
    private final List<String> logs = new ArrayList<>();
    private final GoncBridge bridge = new MobileGoncBridge();
    private VpnServerController vpnServer;
    private VpnClientController vpnClient;
    private SendController sendController;
    private ReceiveController receiveController;
    private java.util.function.Consumer<String> pendingScanCallback;
    private boolean pendingScanIsProfile;

    private LinearLayout root;
    private boolean sendMode = true;
    private boolean vpnMode;
    private boolean vpnServerMode;
    private boolean pendingVpnStartAfterBatterySettings;
    private boolean activityExpanded;
    private long pauseAutoRenderUntilMs;
    private long lastBackgroundRenderMs;
    private boolean backgroundRenderPending;
    private TextView activitySummaryTextView;
    private LinearLayout activityLogContentView;
    private TextView activityP2PStatusValueView;
    private TextView activitySpeedValueView;
    private TextView activityConnectionsValueView;
    private TextView activityNetworkValueView;
    private TextView activityRouteValueView;
    private TextView activityPeerValueView;
    private TextView activityPeerIpv6ValueView;
    private TextView activityEndpointValueView;

    private static final int METRIC_REF_NONE = 0;
    private static final int METRIC_REF_P2P_STATUS = 1;
    private static final int METRIC_REF_SPEED = 2;
    private static final int METRIC_REF_CONNECTIONS = 3;
    private static final int METRIC_REF_NETWORK = 4;
    private static final int METRIC_REF_PEER = 5;
    private static final int METRIC_REF_ENDPOINT = 6;
    private static final int METRIC_REF_ROUTE = 7;
    private static final int METRIC_REF_PEER_IPV6 = 8;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        GoncCrashReporter.install(this);
        RetainedModules retained = (RetainedModules) getLastNonConfigurationInstance();
        boolean reusedModules = retained != null;
        if (reusedModules) {
            vpnServer = retained.vpnServer;
            vpnClient = retained.vpnClient;
            sendController = retained.sendController;
            receiveController = retained.receiveController;
            vpnServer.attach(this);
            vpnClient.attach(this);
            sendController.attach(this);
            receiveController.attach(this);
        } else {
            vpnServer = new VpnServerController(this);
            vpnClient = new VpnClientController(this);
            sendController = new SendController(this);
            receiveController = new ReceiveController(this);
            vpnClient.load();
            vpnServer.load();
        }
        buildRoot();
        if (!reusedModules) {
            vpnClient.register();
        }
        String lastCrash = GoncCrashReporter.lastCrash(this);
        if (!lastCrash.trim().isEmpty()) {
            appendLog("error", "Previous crash:\n" + lastCrash);
            activityExpanded = true;
        }
        handleIncomingIntent(getIntent());
        render();
        if (!lastCrash.trim().isEmpty()) {
            mainHandler.post(this::showPreviousCrashDialog);
        }
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleIncomingIntent(intent);
        render();
    }

    @Override
    protected void onResume() {
        super.onResume();
        vpnClient.syncLogs();
        renderAfterBackgroundUpdate();
        if (!pendingVpnStartAfterBatterySettings) {
            return;
        }
        pendingVpnStartAfterBatterySettings = false;
        if (isIgnoringBatteryOptimizations()) {
            appendLog("info", "Battery optimization ignored; continuing VPN start");
            requestStartVpn(true);
        } else {
            appendLog("warn", "Battery optimization still enabled for this app");
            Toast.makeText(this, R.string.toast_battery_optimization_still_enabled, Toast.LENGTH_SHORT).show();
        }
    }

    @Override
    public void onBackPressed() {
        if (isAnySessionRunning()) {
            showRunningTaskBackDialog();
            return;
        }
        resetTransientStateForFreshLaunch();
        finish();
    }

    private void showRunningTaskBackDialog() {
        new AlertDialog.Builder(this)
                .setTitle(R.string.running_task_back_title)
                .setMessage(R.string.running_task_back_message)
                .setPositiveButton(R.string.run_in_background, (dialog, which) -> {
                    moveTaskToBack(true);
                    Toast.makeText(this, R.string.toast_transfer_kept_running, Toast.LENGTH_SHORT).show();
                })
                .setNegativeButton(R.string.end_tasks, (dialog, which) -> {
                    endAllTasksAndExit();
                })
                .setNeutralButton(R.string.cancel, null)
                .show();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQUEST_SCAN_QR) {
            String result = data == null ? null : data.getStringExtra(Intents.Scan.RESULT);
            if (result != null && !result.trim().isEmpty()) {
                if (pendingScanCallback != null) {
                    java.util.function.Consumer<String> callback = pendingScanCallback;
                    pendingScanCallback = null;
                    callback.accept(result.trim());
                }
            }
            return;
        }
        if (requestCode == REQUEST_VPN_PERMISSION) {
            if (resultCode == RESULT_OK) {
                GoncCrashReporter.appendLog(this, "info", "VPN permission granted");
                startVpnService();
            } else {
                GoncCrashReporter.appendLog(this, "warn", "VPN permission denied");
                Toast.makeText(this, R.string.toast_vpn_permission_denied, Toast.LENGTH_SHORT).show();
            }
            return;
        }
        if (requestCode == REQUEST_OPEN_DOCUMENT && resultCode == RESULT_OK && data != null) {
            List<Uri> uris = collectUris(data);
            addUris(uris, data);
            sendMode = true;
            render();
        } else if (requestCode == REQUEST_OPEN_SEND_TREE && resultCode == RESULT_OK && data != null && data.getData() != null) {
            addTreeUri(data.getData(), data);
            sendMode = true;
            render();
        } else if (requestCode == REQUEST_OPEN_SAVE_TREE && resultCode == RESULT_OK && data != null && data.getData() != null) {
            Uri saveTreeUri = data.getData();
            int flags = data.getFlags() & (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            try {
                if (flags != 0) {
                    getContentResolver().takePersistableUriPermission(saveTreeUri, flags);
                }
            } catch (RuntimeException ignored) {
            }
            String saveLocationLabel = HttpReceiver.displayName(this, saveTreeUri);
            receiveController.setSaveLocation(saveTreeUri, saveLocationLabel);
            appendLog("info", "Save location selected: " + saveLocationLabel);
            render();
        }
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == REQUEST_CAMERA_PERMISSION) {
            if (grantResults.length > 0 && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                launchQrScanner();
            } else {
                Toast.makeText(this, R.string.toast_camera_permission, Toast.LENGTH_SHORT).show();
            }
        } else if (requestCode == REQUEST_STORAGE_PERMISSION) {
            if (grantResults.length == 0 || grantResults[0] != PackageManager.PERMISSION_GRANTED) {
                Toast.makeText(this, R.string.toast_storage_permission, Toast.LENGTH_SHORT).show();
            }
        } else if (requestCode == REQUEST_NOTIFICATION_PERMISSION) {
            // Re-render so the ongoing notification appears now that it's allowed.
            if (grantResults.length > 0 && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                refreshForegroundService();
            }
        }
    }

    @Override
    public Object onRetainNonConfigurationInstance() {
        // Keep the extracted module controllers (and their live sessions)
        // alive across a configuration-change recreation; the new Activity
        // re-attaches itself via attach().
        return new RetainedModules(vpnServer, vpnClient, sendController, receiveController);
    }

    @Override
    protected void onDestroy() {
        // On a config change the controllers are retained, so leave them and
        // their sessions running; only tear down on a real destroy.
        if (!isChangingConfigurations()) {
            vpnClient.unregister();
            sendController.shutdown();
            vpnServer.shutdown();
            receiveController.shutdown();
        }
        super.onDestroy();
    }

    private static final class RetainedModules {
        final VpnServerController vpnServer;
        final VpnClientController vpnClient;
        final SendController sendController;
        final ReceiveController receiveController;

        RetainedModules(VpnServerController vpnServer, VpnClientController vpnClient, SendController sendController, ReceiveController receiveController) {
            this.vpnServer = vpnServer;
            this.vpnClient = vpnClient;
            this.sendController = sendController;
            this.receiveController = receiveController;
        }
    }

    private void buildRoot() {
        Window window = getWindow();
        window.setStatusBarColor(Color.rgb(246, 248, 251));
        window.setNavigationBarColor(Color.WHITE);
        window.getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR);

        ScrollView scrollView = new ScrollView(this);
        scrollView.setFillViewport(true);
        scrollView.setBackgroundColor(Color.rgb(246, 248, 251));

        root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(18), dp(22), dp(18), dp(24));
        scrollView.addView(root, new ScrollView.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
        ));
        setContentView(scrollView);

        // Pad for the status/navigation bars and, crucially, the IME. When the
        // soft keyboard appears, its inset is added to the bottom padding so the
        // ScrollView shrinks and (together with windowSoftInputMode=adjustResize)
        // scrolls the focused field above the keyboard instead of being covered.
        ViewCompat.setOnApplyWindowInsetsListener(scrollView, (v, insets) -> {
            Insets bars = insets.getInsets(WindowInsetsCompat.Type.systemBars());
            Insets ime = insets.getInsets(WindowInsetsCompat.Type.ime());
            int imeBottom = ime.bottom;
            // Bottom padding = keyboard height gives the ScrollView extra scroll
            // range so content can move fully above the keyboard.
            v.setPadding(bars.left, bars.top, bars.right, Math.max(bars.bottom, imeBottom));
            if (imeBottom > 0) {
                // In edge-to-edge the window isn't resized, so ScrollView's own
                // "is it visible" math (which uses full height) won't scroll the
                // focused field up. Do it explicitly by the actual overlap.
                v.post(() -> revealFocusedAboveKeyboard((ScrollView) v, imeBottom));
            }
            return insets;
        });
    }

    /**
     * Scroll the currently focused field up by however much the keyboard overlaps
     * it. Window-coordinate math (not ScrollView's visibility heuristics) so it
     * works under edge-to-edge where the window is not resized for the IME.
     */
    private void revealFocusedAboveKeyboard(ScrollView scrollView, int imeBottom) {
        View focused = scrollView.findFocus();
        if (focused == null) {
            return;
        }
        int[] focusedLoc = new int[2];
        focused.getLocationInWindow(focusedLoc);
        int focusedBottom = focusedLoc[1] + focused.getHeight();

        int[] scrollLoc = new int[2];
        scrollView.getLocationInWindow(scrollLoc);
        int keyboardTop = scrollLoc[1] + scrollView.getHeight() - imeBottom;

        int overlap = focusedBottom - keyboardTop + dp(12);
        if (overlap > 0) {
            scrollView.smoothScrollBy(0, overlap);
        }
    }

    private void render() {
        root.removeAllViews();
        root.addView(header());
        root.addView(modeSwitch());
        if (vpnServerMode) {
            root.addView(vpnServer.panel());
        } else if (vpnMode) {
            root.addView(vpnClient.panel());
        } else if (sendMode) {
            root.addView(sendController.panel());
        } else {
            root.addView(receiveController.panel());
        }
        root.addView(logPanel());
    }

    private void renderAfterBackgroundUpdate() {
        if (System.currentTimeMillis() < pauseAutoRenderUntilMs) {
            return;
        }
        if (isRemoteFilesPanelVisible()) {
            updateBackgroundDynamicViews();
            return;
        }
        render();
    }

    private boolean isRemoteFilesPanelVisible() {
        return !vpnMode && !vpnServerMode && !sendMode && receiveController.shouldShowRemoteFilesPanel();
    }

    private void updateBackgroundDynamicViews() {
        receiveController.updateDynamicViews();
        if (activitySummaryTextView != null) {
            activitySummaryTextView.setText(activitySummary(currentMetrics()));
        }
        updateActivityMetricViews();
        updateActivityLogViews();
    }

    private void pauseAutoRender() {
        pauseAutoRenderUntilMs = System.currentTimeMillis() + 15000;
    }

    private View header() {
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        header.setPadding(0, 0, 0, dp(12));

        ImageView mark = new ImageView(this);
        mark.setImageResource(R.mipmap.ic_launcher);
        mark.setAdjustViewBounds(true);
        mark.setScaleType(ImageView.ScaleType.FIT_CENTER);
        header.addView(mark, new LinearLayout.LayoutParams(dp(46), dp(46)));

        LinearLayout titles = column();
        titles.setPadding(dp(12), 0, 0, 0);
        titles.addView(text(getString(R.string.app_name), 19, ink(), Typeface.BOLD));
        titles.addView(text(getString(R.string.app_subtitle), 13, muted(), Typeface.NORMAL));
        header.addView(titles, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

        Button source = ghostButton(getString(R.string.source));
        source.setOnClickListener(v -> showSourceDialog());
        header.addView(source, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));

        return header;
    }

    private View modeSwitch() {
        LinearLayout box = column();
        box.setPadding(dp(4), dp(4), dp(4), dp(4));
        box.setBackground(rounded(Color.WHITE, dp(8), Color.rgb(216, 226, 238), 1));

        Button send = modeButton(getString(R.string.send_files), !vpnMode && !vpnServerMode && sendMode);
        send.setOnClickListener(v -> {
            vpnMode = false;
            vpnServerMode = false;
            sendMode = true;
            render();
        });
        Button receive = modeButton(getString(R.string.receive_files), !vpnMode && !vpnServerMode && !sendMode);
        receive.setOnClickListener(v -> {
            vpnMode = false;
            vpnServerMode = false;
            sendMode = false;
            render();
        });
        Button vpn = modeButton(getString(R.string.vpn_tunnel), vpnMode);
        vpn.setOnClickListener(v -> {
            vpnMode = true;
            vpnServerMode = false;
            render();
        });
        Button vpnServer = modeButton(getString(R.string.vpn_server), vpnServerMode);
        vpnServer.setOnClickListener(v -> {
            vpnMode = false;
            vpnServerMode = true;
            render();
        });

        LinearLayout topRow = row();
        topRow.addView(send, new LinearLayout.LayoutParams(0, dp(42), 1));
        topRow.addView(receive, new LinearLayout.LayoutParams(0, dp(42), 1));
        LinearLayout bottomRow = row();
        bottomRow.addView(vpn, new LinearLayout.LayoutParams(0, dp(42), 1));
        bottomRow.addView(vpnServer, new LinearLayout.LayoutParams(0, dp(42), 1));

        box.addView(topRow, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        LinearLayout.LayoutParams bottomParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        bottomParams.setMargins(0, dp(4), 0, 0);
        box.addView(bottomRow, bottomParams);
        box.setLayoutParams(blockParams());
        return box;
    }

    private View metricBox(String label, String value) {
        return metricBox(label, value, METRIC_REF_NONE);
    }

    private View metricBox(String label, String value, int ref) {
        LinearLayout box = column();
        box.setPadding(dp(10), dp(8), dp(10), dp(8));
        box.setBackground(rounded(Color.rgb(251, 253, 255), dp(7), Color.rgb(226, 232, 240), 1));
        box.addView(text(label, 11, muted(), Typeface.BOLD));
        TextView content = text(value, 14, ink(), Typeface.BOLD);
        bindMetricRef(ref, content);
        content.setTextIsSelectable(true);
        content.setOnLongClickListener(v -> {
            pauseAutoRender();
            String currentValue = content.getText() == null ? "" : content.getText().toString();
            if (!currentValue.trim().isEmpty() && !"-".equals(currentValue.trim())) {
                copyText(label, currentValue);
                Toast.makeText(this, R.string.toast_value_copied, Toast.LENGTH_SHORT).show();
            }
            return false;
        });
        box.addView(content);
        return box;
    }

    private String routeLabel(String mode) {
        return ui.routeLabel(mode);
    }

    private String displayRouteMetric(TransferMetrics metrics) {
        String route = routeLabel(metrics.routeMode);
        return route.isEmpty() ? "-" : route;
    }

    private View logPanel() {
        LinearLayout card = card();
        LinearLayout title = row();
        title.setGravity(Gravity.CENTER_VERTICAL);
        title.addView(text(getString(R.string.activity), 16, ink(), Typeface.BOLD), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        LinearLayout titleActions = row();
        titleActions.setGravity(Gravity.RIGHT | Gravity.CENTER_VERTICAL);
        Button toggle = ghostButton(activityExpanded ? getString(R.string.hide) : getString(R.string.show));
        toggle.setOnClickListener(v -> {
            activityExpanded = !activityExpanded;
            render();
        });
        titleActions.addView(toggle, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        titleActions.addView(logOptionsButton(), actionParams(dp(38)));
        title.addView(titleActions, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        card.addView(title);

        if (!activityExpanded) {
            clearActivityMetricRefs();
            activityLogContentView = null;
            String summary = activitySummary(currentMetrics());
            TextView summaryView = text(summary, 13, muted(), Typeface.BOLD);
            activitySummaryTextView = summaryView;
            card.addView(summaryView, blockParams(dp(8)));
            return card;
        }
        activitySummaryTextView = null;

        card.addView(activityMetrics(), blockParams(dp(8)));

        LinearLayout logContent = column();
        activityLogContentView = logContent;
        updateActivityLogViews();
        card.addView(logContent, blockParams(dp(8)));
        return card;
    }

    private View logOptionsButton() {
        Button more = ghostButton("⋮");
        more.setTextSize(22);
        more.setMinWidth(0);
        more.setMinimumWidth(0);
        more.setPadding(0, 0, 0, dp(2));
        more.setOnClickListener(v -> showLogOptions(more));
        return more;
    }

    private void showLogOptions(View anchor) {
        PopupMenu menu = new PopupMenu(this, anchor);
        menu.getMenu().add(0, 1, 0, R.string.copy);
        menu.getMenu().add(0, 2, 1, R.string.share_diagnostics);
        menu.getMenu().add(0, 3, 2, R.string.clear);
        menu.setOnMenuItemClickListener(item -> {
            if (item.getItemId() == 1) {
                copyLogs();
                return true;
            }
            if (item.getItemId() == 2) {
                shareDiagnostics();
                return true;
            }
            if (item.getItemId() == 3) {
                logs.clear();
                GoncCrashReporter.clearDiagnostics(this);
                render();
                return true;
            }
            return false;
        });
        menu.show();
    }

    private void updateActivityLogViews() {
        if (!activityExpanded || activityLogContentView == null) {
            return;
        }
        activityLogContentView.removeAllViews();
        if (logs.isEmpty()) {
            activityLogContentView.addView(text(getString(R.string.events_will_appear), 13, muted(), Typeface.NORMAL));
            return;
        }

        LinearLayout logBox = column();
        logBox.setPadding(dp(12), dp(10), dp(12), dp(10));
        logBox.setBackground(rounded(Color.rgb(16, 24, 38), dp(8), 0, 0));
        int start = Math.max(0, logs.size() - MAX_VISIBLE_ACTIVITY_LOGS);
        for (int i = start; i < logs.size(); i++) {
            TextView line = text(logs.get(i), 12, Color.rgb(219, 231, 246), Typeface.NORMAL);
            line.setTypeface(Typeface.MONOSPACE);
            line.setTextIsSelectable(true);
            logBox.addView(line);
        }
        activityLogContentView.addView(logBox);
    }

    private View activityMetrics() {
        TransferMetrics metrics = currentMetrics();
        clearActivityMetricRefs();
        LinearLayout box = column();
        LinearLayout row1 = row();
        row1.addView(metricBox(getString(R.string.p2p_status), displayStatusLabel(displayStatus(metrics)), METRIC_REF_P2P_STATUS), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row1.addView(metricBox(getString(R.string.speed), formatRate(currentSpeed(metrics)), METRIC_REF_SPEED), metricParams());
        box.addView(row1);

        LinearLayout row2 = row();
        if (!vpnMode && (sendMode || vpnServerMode)) {
            row2.addView(metricBox(getString(R.string.connections), String.valueOf(metrics.connectedCount), METRIC_REF_CONNECTIONS), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            row2.addView(metricBox(getString(R.string.network), emptyDash(metrics.network), METRIC_REF_NETWORK), metricParams());
        } else {
            row2.addView(metricBox(getString(R.string.network), emptyDash(metrics.network), METRIC_REF_NETWORK), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            row2.addView(metricBox(getString(R.string.connection_route), displayRouteMetric(metrics), METRIC_REF_ROUTE), metricParams());
        }
        box.addView(row2, blockParams(dp(8)));
        box.addView(metricBox(getString(R.string.peer), emptyDash(metrics.peer), METRIC_REF_PEER), blockParams(dp(8)));
        if (vpnMode) {
            box.addView(metricBox(getString(R.string.peer_ipv6), displayPeerIpv6(GoncVpnState.peerIpv6()), METRIC_REF_PEER_IPV6), blockParams(dp(8)));
        }
        if (vpnMode && !GoncVpnState.endpoint().trim().isEmpty()) {
            box.addView(metricBox(getString(R.string.vpn_endpoint), GoncVpnState.endpoint(), METRIC_REF_ENDPOINT), blockParams(dp(8)));
        } else if (!receiveController.endpoint().trim().isEmpty()) {
            box.addView(metricBox(getString(R.string.file_endpoint), receiveController.endpoint(), METRIC_REF_ENDPOINT), blockParams(dp(8)));
        }
        return box;
    }

    private void bindMetricRef(int ref, TextView view) {
        if (ref == METRIC_REF_P2P_STATUS) {
            activityP2PStatusValueView = view;
        } else if (ref == METRIC_REF_SPEED) {
            activitySpeedValueView = view;
        } else if (ref == METRIC_REF_CONNECTIONS) {
            activityConnectionsValueView = view;
        } else if (ref == METRIC_REF_NETWORK) {
            activityNetworkValueView = view;
        } else if (ref == METRIC_REF_ROUTE) {
            activityRouteValueView = view;
        } else if (ref == METRIC_REF_PEER) {
            activityPeerValueView = view;
        } else if (ref == METRIC_REF_PEER_IPV6) {
            activityPeerIpv6ValueView = view;
        } else if (ref == METRIC_REF_ENDPOINT) {
            activityEndpointValueView = view;
        }
    }

    private void clearActivityMetricRefs() {
        activityP2PStatusValueView = null;
        activitySpeedValueView = null;
        activityConnectionsValueView = null;
        activityNetworkValueView = null;
        activityRouteValueView = null;
        activityPeerValueView = null;
        activityPeerIpv6ValueView = null;
        activityEndpointValueView = null;
    }

    private void updateActivityMetricViews() {
        TransferMetrics metrics = currentMetrics();
        if (activityP2PStatusValueView != null) {
            activityP2PStatusValueView.setText(displayStatusLabel(displayStatus(metrics)));
        }
        if (activitySpeedValueView != null) {
            activitySpeedValueView.setText(formatRate(currentSpeed(metrics)));
        }
        if (activityConnectionsValueView != null) {
            activityConnectionsValueView.setText(String.valueOf(metrics.connectedCount));
        }
        if (activityNetworkValueView != null) {
            activityNetworkValueView.setText(emptyDash(metrics.network));
        }
        if (activityRouteValueView != null) {
            activityRouteValueView.setText(displayRouteMetric(metrics));
        }
        if (activityPeerValueView != null) {
            activityPeerValueView.setText(emptyDash(metrics.peer));
        }
        if (activityPeerIpv6ValueView != null) {
            activityPeerIpv6ValueView.setText(displayPeerIpv6(GoncVpnState.peerIpv6()));
        }
        if (activityEndpointValueView != null) {
            activityEndpointValueView.setText(vpnMode ? GoncVpnState.endpoint() : receiveController.endpoint());
        }
    }

    private String displayPeerIpv6(String value) {
        String clean = value == null ? "" : value.trim().toLowerCase(java.util.Locale.ROOT);
        if ("disabled".equals(clean)) {
            return getString(R.string.peer_ipv6_disabled);
        }
        if ("waiting".equals(clean)) {
            return getString(R.string.peer_ipv6_waiting);
        }
        if ("checking".equals(clean)) {
            return getString(R.string.peer_ipv6_checking);
        }
        if ("available".equals(clean)) {
            return getString(R.string.peer_ipv6_available);
        }
        if ("unavailable".equals(clean)) {
            return getString(R.string.peer_ipv6_unavailable);
        }
        return emptyDash(value);
    }

    private TextView sectionTitle(String title) {
        return ui.sectionTitle(title);
    }

    private View sectionBoundaryTitle(String title, boolean separated) {
        return ui.sectionBoundaryTitle(title, separated);
    }

    private View sectionDivider() {
        return ui.sectionDivider();
    }

    private void openDocumentPicker() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("*/*");
        intent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        startActivityForResult(intent, REQUEST_OPEN_DOCUMENT);
    }

    private void openSendFolderPicker() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE);
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        startActivityForResult(intent, REQUEST_OPEN_SEND_TREE);
    }

    private void openSaveLocationPicker() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE);
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        startActivityForResult(intent, REQUEST_OPEN_SAVE_TREE);
    }

    private void endAllTasksAndExit() {
        sendController.endTask();
        receiveController.endTask();
        vpnServer.endTask();
        if (GoncVpnState.isRunning()) {
            stopVpnService();
        }

        resetTransientStateForFreshLaunch();
        refreshForegroundService();
        finish();
    }

    private void resetTransientStateForFreshLaunch() {
        logs.clear();

        sendMode = true;
        vpnMode = false;
        vpnServerMode = false;
        vpnClient.resetForFreshLaunch();
        sendController.resetForFreshLaunch();
        receiveController.resetForFreshLaunch();
        vpnServer.resetForFreshLaunch();
        activityExpanded = false;
    }

    private void requestStartVpn() {
        requestStartVpn(false);
    }

    private void requestStartVpn(boolean skipBatteryOptimizationPrompt) {
        VpnProfile profile = vpnClient.currentProfile();
        String passphrase = profile.passphrase.trim();
        if (passphrase.isEmpty()) {
            Toast.makeText(this, R.string.toast_passphrase_required, Toast.LENGTH_SHORT).show();
            return;
        }
        if (Passwords.isWeak(passphrase)) {
            Toast.makeText(this, R.string.toast_passphrase_weak, Toast.LENGTH_SHORT).show();
            return;
        }
        if (!skipBatteryOptimizationPrompt && shouldSuggestBatteryOptimizationSettings()) {
            showBatteryOptimizationDialog();
            return;
        }
        if (profile.tunnelOnly) {
            // Tunnel-only never establishes the system VPN interface, so it needs no
            // VpnService consent — start the (specialUse) foreground service directly.
            startVpnService();
            return;
        }
        requestVpnPermissionOrStart();
    }

    private void requestVpnPermissionOrStart() {
        Intent prepare = VpnService.prepare(this);
        if (prepare != null) {
            GoncCrashReporter.appendLog(this, "info", "Requesting VPN permission");
            startActivityForResult(prepare, REQUEST_VPN_PERMISSION);
            return;
        }
        GoncCrashReporter.appendLog(this, "info", "VPN permission already granted");
        startVpnService();
    }

    private boolean shouldSuggestBatteryOptimizationSettings() {
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.M && !isIgnoringBatteryOptimizations();
    }

    private boolean isIgnoringBatteryOptimizations() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) {
            return true;
        }
        PowerManager manager = (PowerManager) getSystemService(Context.POWER_SERVICE);
        return manager != null && manager.isIgnoringBatteryOptimizations(getPackageName());
    }

    private void showBatteryOptimizationDialog() {
        new AlertDialog.Builder(this)
                .setTitle(R.string.battery_optimization_title)
                .setMessage(R.string.battery_optimization_message)
                .setPositiveButton(R.string.open_battery_settings, (dialog, which) -> openBatteryOptimizationSettings())
                .setNegativeButton(R.string.continue_anyway, (dialog, which) -> requestStartVpn(true))
                .setNeutralButton(R.string.cancel, null)
                .show();
    }

    private void openBatteryOptimizationSettings() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) {
            requestStartVpn(true);
            return;
        }
        pendingVpnStartAfterBatterySettings = true;
        appendLog("info", "Requesting ignore battery optimization");
        Intent request = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS);
        request.setData(Uri.parse("package:" + getPackageName()));
        if (startBatterySettingsIntent(request)) {
            return;
        }
        Intent list = new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS);
        if (startBatterySettingsIntent(list)) {
            return;
        }
        Intent details = new Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS);
        details.setData(Uri.parse("package:" + getPackageName()));
        if (!startBatterySettingsIntent(details)) {
            pendingVpnStartAfterBatterySettings = false;
            Toast.makeText(this, R.string.toast_open_battery_settings_failed, Toast.LENGTH_SHORT).show();
        }
    }

    private boolean startBatterySettingsIntent(Intent intent) {
        try {
            startActivity(intent);
            return true;
        } catch (RuntimeException error) {
            GoncCrashReporter.appendLog(this, "warn", "Cannot open battery settings: " + error.getMessage());
            return false;
        }
    }

    private void startVpnService() {
        VpnProfile profile = vpnClient.currentProfile();
        vpnClient.normalizeAndSaveCurrent();
        Intent intent = new Intent(this, GoncVpnService.class);
        intent.setAction(GoncVpnService.ACTION_START);
        intent.putExtra(GoncVpnService.EXTRA_PASSWORD, profile.passphrase.trim());
        intent.putExtra(GoncVpnService.EXTRA_USE_UDP, profile.useUdp);
        intent.putExtra(GoncVpnService.EXTRA_ENABLE_IPV6, profile.routeIpv6);
        intent.putExtra(GoncVpnService.EXTRA_DNS_SERVERS, profile.dnsServers);
        intent.putExtra(GoncVpnService.EXTRA_ROUTE_CIDRS, profile.routeCidrs);
        intent.putExtra(GoncVpnService.EXTRA_LINK_CONFIG, profile.linkConfig == null ? "" : profile.linkConfig.trim());
        intent.putExtra(GoncVpnService.EXTRA_EXTRA_ARGS, profile.extraArgs == null ? "" : profile.extraArgs.trim());
        intent.putExtra(GoncVpnService.EXTRA_TUNNEL_ONLY, profile.tunnelOnly);
        appendLog("info", "VPN start requested: " + profile.displayName(this) + (profile.tunnelOnly ? " (tunnel only)" : ""));
        GoncVpnState.startConnecting(profile.displayName(this));
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(intent);
        } else {
            startService(intent);
        }
        refreshForegroundService();
        render();
    }

    private void stopVpnService() {
        if (!GoncVpnState.isRunning()) {
            GoncVpnState.setStatus(GoncVpnState.DISCONNECTED);
            render();
            return;
        }
        Intent intent = new Intent(this, GoncVpnService.class);
        intent.setAction(GoncVpnService.ACTION_STOP);
        appendLog("warn", "VPN stop requested");
        GoncVpnState.setStatus(GoncVpnState.STOPPING);
        startService(intent);
        refreshForegroundService();
        render();
    }

    @Override
    public void showPassphraseQr(String passphrase) {
        if (passphrase.isEmpty()) {
            Toast.makeText(this, R.string.toast_passphrase_empty, Toast.LENGTH_SHORT).show();
            return;
        }
        try {
            Dialog dialog = new Dialog(this);
            dialog.requestWindowFeature(Window.FEATURE_NO_TITLE);
            LinearLayout box = column();
            box.setPadding(dp(18), dp(18), dp(18), dp(18));
            box.setBackground(rounded(Color.WHITE, dp(8), 0, 0));
            TextView title = text(getString(R.string.passphrase_qr), 16, ink(), Typeface.BOLD);
            title.setGravity(Gravity.CENTER);
            box.addView(title);

            Bitmap bitmap = QrCodes.encode(passphrase, dp(260));
            ImageView image = new ImageView(this);
            image.setImageBitmap(bitmap);
            image.setAdjustViewBounds(true);
            image.setBackgroundColor(Color.WHITE);
            image.setLongClickable(true);
            image.setOnLongClickListener(v -> {
                copyPassphraseFromQrDialog(passphrase);
                return true;
            });
            box.addView(image, blockParams(dp(12)));

            TextView hint = text(getString(R.string.passphrase_qr_copy_hint), 12, muted(), Typeface.NORMAL);
            hint.setGravity(Gravity.CENTER);
            box.addView(hint, blockParams(dp(4)));

            TextView value = text(passphrase, 14, ink(), Typeface.BOLD);
            value.setTypeface(Typeface.MONOSPACE, Typeface.BOLD);
            value.setTextIsSelectable(true);
            value.setGravity(Gravity.CENTER);
            value.setSingleLine(false);
            value.setPadding(dp(10), dp(8), dp(10), dp(8));
            value.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));
            value.setLongClickable(true);
            value.setOnLongClickListener(v -> {
                copyPassphraseFromQrDialog(passphrase);
                return true;
            });
            box.addView(value, blockParams(dp(8)));

            Button close = secondaryButton(getString(R.string.close));
            close.setOnClickListener(v -> dialog.dismiss());
            box.addView(close, blockParams(dp(12)));

            dialog.setContentView(box);
            dialog.show();
        } catch (Exception error) {
            Toast.makeText(this, R.string.toast_qr_failed, Toast.LENGTH_SHORT).show();
            appendLog("error", error.getMessage() == null ? error.toString() : error.getMessage());
            render();
        }
    }

    private void copyPassphraseFromQrDialog(String passphrase) {
        copyText("Gonc passphrase", passphrase);
        appendLog("info", "Passphrase copied");
        Toast.makeText(this, R.string.toast_passphrase_copied, Toast.LENGTH_SHORT).show();
    }

    private void showSourceDialog() {
        Dialog dialog = new Dialog(this);
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE);
        dialog.setCanceledOnTouchOutside(true);
        LinearLayout box = column();
        box.setPadding(dp(18), dp(18), dp(18), dp(18));
        box.setBackground(rounded(Color.WHITE, dp(8), 0, 0));

        TextView title = text(getString(R.string.source_title), 16, ink(), Typeface.BOLD);
        title.setGravity(Gravity.CENTER);
        box.addView(title);

        TextView version = text(getString(R.string.app_name) + " v" + appVersionName(), 12, muted(), Typeface.NORMAL);
        version.setGravity(Gravity.CENTER);
        box.addView(version, blockParams(dp(2)));

        TextView desc = text(getString(R.string.source_description), 13, muted(), Typeface.NORMAL);
        desc.setSingleLine(false);
        box.addView(desc, blockParams(dp(10)));

        box.addView(sourceLinkView(GONC_URL), blockParams(dp(8)));
        box.addView(sourceLinkView(SOURCE_URL), blockParams(dp(6)));

        dialog.setContentView(box);
        dialog.show();
    }

    private TextView sourceLinkView(String url) {
        TextView view = text(url, 13, Color.rgb(32, 101, 165), Typeface.BOLD);
        view.setSingleLine(false);
        view.setPadding(dp(10), dp(8), dp(10), dp(8));
        view.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));
        view.setOnClickListener(v -> openSourceUrl(url));
        view.setOnLongClickListener(v -> {
            copyText(getString(R.string.source_title), url);
            Toast.makeText(this, R.string.toast_source_copied, Toast.LENGTH_SHORT).show();
            return true;
        });
        return view;
    }

    private void openSourceUrl(String url) {
        try {
            startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url)));
        } catch (RuntimeException error) {
            copyText(getString(R.string.source_title), url);
            Toast.makeText(this, R.string.toast_source_copied, Toast.LENGTH_SHORT).show();
        }
    }

    private String appVersionName() {
        try {
            String name = getPackageManager().getPackageInfo(getPackageName(), 0).versionName;
            return name == null ? "" : name;
        } catch (Exception error) {
            return "";
        }
    }

    private void launchQrScanner() {
        Intent intent = new Intent(this, QrScanActivity.class);
        intent.setAction(Intents.Scan.ACTION);
        intent.putExtra(Intents.Scan.MODE, Intents.Scan.QR_CODE_MODE);
        intent.putExtra(Intents.Scan.PROMPT_MESSAGE, getString(pendingScanIsProfile ? R.string.scan_vpn_profile_prompt : R.string.scan_prompt));
        intent.putExtra(Intents.Scan.BEEP_ENABLED, false);
        startActivityForResult(intent, REQUEST_SCAN_QR);
    }

    private void copyLogs() {
        if (logs.isEmpty()) {
            Toast.makeText(this, R.string.toast_no_logs, Toast.LENGTH_SHORT).show();
            return;
        }
        StringBuilder builder = new StringBuilder();
        for (String line : logs) {
            builder.append(line).append('\n');
        }
        copyText("Gonc activity", builder.toString().trim());
        Toast.makeText(this, R.string.toast_activity_copied, Toast.LENGTH_SHORT).show();
    }

    private void showPreviousCrashDialog() {
        String crash = GoncCrashReporter.lastCrash(this);
        if (crash.trim().isEmpty() || isFinishing()) {
            return;
        }
        new AlertDialog.Builder(this)
                .setTitle(R.string.previous_crash_title)
                .setMessage(R.string.previous_crash_message)
                .setPositiveButton(R.string.copy_diagnostics, (dialog, which) -> {
                    copyDiagnostics();
                    GoncCrashReporter.clearLastCrash(this);
                })
                .setNegativeButton(R.string.share_diagnostics, (dialog, which) -> {
                    shareDiagnostics();
                    GoncCrashReporter.clearLastCrash(this);
                })
                .setNeutralButton(R.string.close, (dialog, which) -> GoncCrashReporter.clearLastCrash(this))
                .show();
    }

    private void copyDiagnostics() {
        copyText("Gonc diagnostics", GoncCrashReporter.diagnostics(this, logs));
        Toast.makeText(this, R.string.toast_diagnostics_copied, Toast.LENGTH_SHORT).show();
    }

    private void shareDiagnostics() {
        String diagnostics = GoncCrashReporter.diagnostics(this, logs);
        Intent intent = new Intent(Intent.ACTION_SEND);
        intent.setType("text/plain");
        intent.putExtra(Intent.EXTRA_SUBJECT, "Gonc Android diagnostics");
        intent.putExtra(Intent.EXTRA_TEXT, diagnostics);
        startActivity(Intent.createChooser(intent, getString(R.string.share_diagnostics)));
    }

    @Override
    public void copyText(String label, String value) {
        ClipboardManager clipboard = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (clipboard != null) {
            clipboard.setPrimaryClip(ClipData.newPlainText(label, value));
        }
    }

    private String currentStatus() {
        if (vpnMode) {
            // Translate the VPN lifecycle status into the shared status vocabulary so
            // the activity summary reads the same as the file modules ("空闲" instead
            // of the raw "Disconnected").
            String status = GoncVpnState.status();
            if (GoncVpnState.CONNECTING.equals(status)) {
                return "starting";
            }
            if (GoncVpnState.CONNECTED.equals(status)) {
                return "connected";
            }
            if (GoncVpnState.ERROR.equals(status)) {
                return "Error";
            }
            return "Idle"; // Disconnected / Stopping → idle
        }
        if (vpnServerMode) {
            return vpnServer.status();
        }
        return sendMode ? sendController.status() : receiveController.status();
    }

    private String statusSummary() {
        TransferMetrics metrics = currentMetrics();
        String state = displayStatus(metrics);
        if ("idle".equals(state)) {
            state = currentStatus();
        }
        state = displayStatusLabel(state);
        if (vpnMode || (!sendMode && !vpnServerMode)) {
            return state + "  " + formatRate(currentSpeed(metrics));
        }
        return state + "  " + metrics.connectedCount + "  " + formatRate(currentSpeed(metrics));
    }

    private String activitySummary(TransferMetrics metrics) {
        String state = displayStatus(metrics);
        if ("idle".equals(state)) {
            state = currentStatus();
        }
        String speed = getString(R.string.activity_summary_speed, formatRate(currentSpeed(metrics)));
        if (vpnMode || (!sendMode && !vpnServerMode)) {
            return activityStateLabel(state, metrics) + " | " + speed;
        }
        return activityStateLabel(state, metrics)
                + " | "
                + getString(R.string.activity_summary_connected, metrics.connectedCount)
                + " | "
                + speed;
    }

    private String activityStateLabel(String state, TransferMetrics metrics) {
        if (state == null) {
            return getString(R.string.activity_state_waiting_connection);
        }
        String clean = state.trim();
        if ("wait".equals(clean)
                || "waiting".equals(clean)
                || "ready".equals(clean)
                || "Ready".equals(clean)
                || "starting".equals(clean)
                || "Preparing".equals(clean)) {
            return getString(R.string.activity_state_waiting_connection);
        }
        if ("connecting".equals(clean)) {
            return getString(R.string.activity_state_new_connection);
        }
        if ("negotiating".equals(clean)) {
            return getString(R.string.activity_state_negotiating);
        }
        if ("connected".equals(clean)) {
            return getString(R.string.activity_state_connection_success);
        }
        return displayStatusLabel(clean);
    }

    private boolean isAnySessionRunning() {
        return sendController.isRunning() || receiveController.isBusy() || vpnServer.isRunning() || GoncVpnState.isRunning();
    }

    private TransferMetrics currentMetrics() {
        if (vpnMode) {
            return vpnClient.metrics();
        }
        if (vpnServerMode) {
            return vpnServer.metrics();
        }
        if (sendMode) {
            return sendController.metrics();
        }
        return receiveController.metrics();
    }

    @Override
    public void updateMetricsFromReport(TransferMetrics metrics, String topic, String status, String network, String mode, String peer) {
        if (status != null && !status.trim().isEmpty()) {
            metrics.p2pStatus = status.trim();
        }
        if (isRouteMode(mode)) {
            metrics.routeMode = mode.trim();
        }
        if (network != null && !network.trim().isEmpty()) {
            metrics.network = network.trim();
        }
        if (peer != null && !peer.trim().isEmpty()) {
            metrics.peer = peer.trim();
        }
        String normalized = normalizeMetricStatus(status);
        String key = topic != null && !topic.trim().isEmpty() ? topic.trim() : emptyDash(peer);
        if ("-".equals(key)) {
            key = mode != null && !mode.trim().isEmpty() ? mode.trim() : "default";
        }
        if (!"wait".equals(normalized)) {
            metrics.sessions.put(key, normalized);
        }
        metrics.recountConnections();
    }

    private boolean isRouteMode(String mode) {
        if (mode == null) {
            return false;
        }
        String clean = mode.trim().toLowerCase(Locale.ROOT);
        return "p2p".equals(clean) || "relay".equals(clean);
    }

    private String normalizeMetricStatus(String status) {
        return ui.normalizeMetricStatus(status);
    }

    @Override
    public void updateMetricsFromLog(TransferMetrics metrics, String message) {
        if (updateTraffic(metrics, message)) {
            return;
        }
        if (message == null) {
            return;
        }
        String compact = message.trim();
        if (compact.contains("Trying P2P Connection") || compact.contains("Exchanging sync message")) {
            metrics.p2pStatus = "connecting";
        } else if (compact.contains("P2P(") && compact.contains("connection established")) {
            metrics.p2pStatus = "negotiating";
            if (compact.contains("P2P(TCP)")) {
                metrics.network = "TCP";
            } else if (compact.contains("P2P(UDP)")) {
                metrics.network = "UDP";
            }
        } else if (compact.contains("P2P failed") || compact.contains("hole punching failed")) {
            metrics.p2pStatus = "failed";
        } else if (compact.contains("P2P stopped")) {
            metrics.markStopped();
        } else if (compact.contains("disconnected")) {
            if (metrics.connectedCount <= 1) {
                metrics.markStopped();
            } else {
                metrics.p2pStatus = "disconnected";
            }
        }
    }

    private boolean updateTraffic(TransferMetrics metrics, String message) {
        TrafficLogParser.Traffic traffic = TrafficLogParser.parse(message);
        if (traffic == null) {
            return false;
        }
        metrics.inBps = traffic.inBps;
        metrics.outBps = traffic.outBps;
        metrics.lastTrafficMs = System.currentTimeMillis();
        return true;
    }

    private double currentSpeed(TransferMetrics metrics) {
        if (vpnMode || vpnServerMode) {
            if (System.currentTimeMillis() - metrics.lastTrafficMs > 4000) {
                return 0;
            }
            return Math.max(metrics.inBps, metrics.outBps);
        }
        if (!sendMode && !receiveController.isDownloading()) {
            return 0;
        }
        if (!sendMode && receiveController.currentDownloadSpeed() > 0) {
            return receiveController.currentDownloadSpeed();
        }
        if (System.currentTimeMillis() - metrics.lastTrafficMs > 4000) {
            return 0;
        }
        return sendMode ? metrics.outBps : Math.max(metrics.inBps, metrics.outBps);
    }

    private String displayStatus(TransferMetrics metrics) {
        return ui.displayStatus(metrics);
    }

    private String displayStatusLabel(String status) {
        if (status == null) {
            return "";
        }
        String clean = status.trim();
        if ("Idle".equals(clean) || "idle".equals(clean)) {
            return getString(R.string.status_idle);
        }
        if ("Preparing".equals(clean)) {
            return getString(R.string.status_preparing);
        }
        if ("Ready".equals(clean) || "ready".equals(clean)) {
            return getString(R.string.status_ready);
        }
        if ("Error".equals(clean) || "error".equals(clean)) {
            return getString(R.string.status_error);
        }
        if ("starting".equals(clean)) {
            return getString(R.string.status_starting);
        }
        if ("connecting".equals(clean)) {
            return getString(R.string.status_connecting);
        }
        if ("negotiating".equals(clean)) {
            return getString(R.string.status_negotiating);
        }
        if ("wait".equals(clean) || "waiting".equals(clean)) {
            return getString(R.string.status_waiting_for_peer);
        }
        if ("connected".equals(clean)) {
            return getString(R.string.status_connected);
        }
        if ("failed".equals(clean)) {
            return getString(R.string.status_failed);
        }
        if ("disconnected".equals(clean)) {
            return getString(R.string.status_disconnected);
        }
        return clean;
    }

    // --- ModuleHost: shared services exposed to per-module controllers ---

    @Override
    public Context context() {
        return this;
    }

    @Override
    public UiKit ui() {
        return ui;
    }

    @Override
    public GoncBridge bridge() {
        return bridge;
    }

    @Override
    public Handler mainHandler() {
        return mainHandler;
    }

    @Override
    public void log(String level, String message) {
        appendLog(level, message);
    }

    @Override
    public void requestRender() {
        render();
    }

    @Override
    public void requestBackgroundRender() {
        // Single coalescing throttle for all modules' high-frequency log/metric
        // updates: render immediately if the window has elapsed, otherwise schedule
        // one trailing flush so the last burst of updates still paints (no "stuck
        // until the next log" gaps). Mirrors ReceiveController#renderDownloadProgress.
        long now = System.currentTimeMillis();
        long elapsed = now - lastBackgroundRenderMs;
        if (elapsed >= BACKGROUND_RENDER_INTERVAL_MS) {
            lastBackgroundRenderMs = now;
            renderAfterBackgroundUpdate();
            return;
        }
        if (backgroundRenderPending) {
            return;
        }
        backgroundRenderPending = true;
        mainHandler.postDelayed(() -> {
            backgroundRenderPending = false;
            lastBackgroundRenderMs = System.currentTimeMillis();
            renderAfterBackgroundUpdate();
        }, Math.max(50, BACKGROUND_RENDER_INTERVAL_MS - elapsed));
    }

    @Override
    public void toast(int messageResId) {
        Toast.makeText(this, messageResId, Toast.LENGTH_SHORT).show();
    }

    @Override
    public void scanPassphrase(java.util.function.Consumer<String> onResult) {
        startScan(false, onResult);
    }

    @Override
    public void scanProfileQr(java.util.function.Consumer<String> onResult) {
        startScan(true, onResult);
    }

    private void startScan(boolean profilePrompt, java.util.function.Consumer<String> onResult) {
        pendingScanCallback = onResult;
        pendingScanIsProfile = profilePrompt;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M
                && checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.CAMERA}, REQUEST_CAMERA_PERMISSION);
            return;
        }
        launchQrScanner();
    }

    @Override
    public void logTransient(String level, String message) {
        appendLog(level, message, false);
    }

    @Override
    public android.content.SharedPreferences prefs() {
        return getSharedPreferences(PREFS, MODE_PRIVATE);
    }

    @Override
    public void startVpnClient() {
        requestStartVpn();
    }

    @Override
    public void stopVpnClient() {
        stopVpnService();
    }

    @Override
    public void pickSendFiles() {
        openDocumentPicker();
    }

    @Override
    public void pickSendFolder() {
        openSendFolderPicker();
    }

    @Override
    public void pickSaveLocation() {
        openSaveLocationPicker();
    }

    @Override
    public boolean ensureLegacyStoragePermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            return true;
        }
        if (checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED) {
            return true;
        }
        requestPermissions(new String[]{Manifest.permission.WRITE_EXTERNAL_STORAGE}, REQUEST_STORAGE_PERMISSION);
        return false;
    }

    @Override
    public boolean isAutoRenderPaused() {
        return System.currentTimeMillis() < pauseAutoRenderUntilMs;
    }

    @Override
    public void refreshForegroundService() {
        // The VPN client is excluded on purpose: it has its own GoncVpnService.
        java.util.EnumMap<GoncForegroundService.Module, GoncForegroundService.State> active =
                new java.util.EnumMap<>(GoncForegroundService.Module.class);
        GoncForegroundService.State state;
        if ((state = sendController.foregroundState()) != null) {
            active.put(GoncForegroundService.Module.SEND, state);
        }
        if ((state = receiveController.foregroundState()) != null) {
            active.put(GoncForegroundService.Module.RECEIVE, state);
        }
        if ((state = vpnServer.foregroundState()) != null) {
            active.put(GoncForegroundService.Module.VPN_SERVER, state);
        }
        if (!active.isEmpty()) {
            ensureNotificationPermission();
        }
        GoncForegroundService.apply(this, active);
    }

    /**
     * Ask for POST_NOTIFICATIONS (Android 13+) the first time a foreground module
     * starts, so the ongoing notification is actually visible. The service runs
     * either way; when the grant arrives we re-render so the notification appears.
     */
    private void ensureNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
            return;
        }
        if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) {
            return;
        }
        if (notificationPermissionRequested) {
            return;
        }
        notificationPermissionRequested = true;
        requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, REQUEST_NOTIFICATION_PERMISSION);
    }

    private void handleIncomingIntent(Intent intent) {
        if (intent == null) {
            return;
        }
        String action = intent.getAction();
        if (Intent.ACTION_MAIN.equals(action)) {
            return;
        }
        List<Uri> uris = collectUris(intent);
        if (!uris.isEmpty()) {
            addUris(uris, intent);
            vpnMode = false;
            vpnServerMode = false;
            sendMode = true;
            appendLog("info", "Received " + uris.size() + " file(s) from Android");
        }
    }

    private List<Uri> collectUris(Intent intent) {
        Map<String, Uri> result = new LinkedHashMap<>();
        String action = intent.getAction();

        if (Intent.ACTION_SEND_MULTIPLE.equals(action)) {
            ArrayList<Uri> streamUris = intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM);
            if (streamUris != null) {
                for (Uri uri : streamUris) {
                    putUri(result, uri);
                }
            }
        }

        if (Intent.ACTION_SEND.equals(action)) {
            Uri streamUri = intent.getParcelableExtra(Intent.EXTRA_STREAM);
            putUri(result, streamUri);
        }

        if (Intent.ACTION_VIEW.equals(action)) {
            putUri(result, intent.getData());
        }

        ClipData clipData = intent.getClipData();
        if (clipData != null) {
            for (int i = 0; i < clipData.getItemCount(); i++) {
                putUri(result, clipData.getItemAt(i).getUri());
            }
        }
        return new ArrayList<>(result.values());
    }

    private void putUri(Map<String, Uri> result, Uri uri) {
        if (uri != null) {
            result.put(uri.toString(), uri);
        }
    }

    private void addUris(List<Uri> uris, Intent sourceIntent) {
        List<ShareItem> items = new ArrayList<>();
        for (Uri uri : uris) {
            takeReadPermission(uri, sourceIntent);
            items.add(loadShareItem(uri));
        }
        sendController.addFiles(items);
    }

    private void addTreeUri(Uri uri, Intent sourceIntent) {
        takeReadPermission(uri, sourceIntent);
        sendController.addFolder(loadTreeShareItem(uri));
    }

    private ShareItem loadShareItem(Uri uri) {
        String name = null;
        long size = -1;
        long lastModified = 0;
        ContentResolver resolver = getContentResolver();
        Cursor cursor = resolver.query(uri, null, null, null, null);
        if (cursor != null) {
            try {
                int nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME);
                int sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE);
                int modifiedIndex = cursor.getColumnIndex(DocumentsContract.Document.COLUMN_LAST_MODIFIED);
                if (cursor.moveToFirst()) {
                    if (nameIndex >= 0) {
                        name = cursor.getString(nameIndex);
                    }
                    if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) {
                        size = cursor.getLong(sizeIndex);
                    }
                    if (modifiedIndex >= 0 && !cursor.isNull(modifiedIndex)) {
                        lastModified = cursor.getLong(modifiedIndex);
                    }
                }
            } finally {
                cursor.close();
            }
        }
        if (name == null || name.trim().isEmpty()) {
            String path = uri.getLastPathSegment();
            name = path == null || path.trim().isEmpty() ? "shared-file" : path;
        }
        String mimeType = resolver.getType(uri);
        return new ShareItem(uri, name, size, mimeType, false, false, lastModified);
    }

    private ShareItem loadTreeShareItem(Uri treeUri) {
        String name = null;
        long lastModified = 0;
        ContentResolver resolver = getContentResolver();
        try {
            String docId = DocumentsContract.getTreeDocumentId(treeUri);
            Uri docUri = DocumentsContract.buildDocumentUriUsingTree(treeUri, docId);
            Cursor cursor = resolver.query(docUri, new String[]{
                    DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                    DocumentsContract.Document.COLUMN_LAST_MODIFIED
            }, null, null, null);
            if (cursor != null) {
                try {
                    if (cursor.moveToFirst()) {
                        name = cursor.getString(0);
                        if (!cursor.isNull(1)) {
                            lastModified = cursor.getLong(1);
                        }
                    }
                } finally {
                    cursor.close();
                }
            }
        } catch (RuntimeException ignored) {
        }
        if (name == null || name.trim().isEmpty()) {
            String path = treeUri.getLastPathSegment();
            name = path == null || path.trim().isEmpty() ? getString(R.string.selected_folder) : path;
        }
        return new ShareItem(treeUri, name, 0, DocumentsContract.Document.MIME_TYPE_DIR, true, true, lastModified);
    }

    private void takeReadPermission(Uri uri, Intent sourceIntent) {
        if (sourceIntent == null) {
            return;
        }
        int flags = sourceIntent.getFlags();
        boolean canPersist = (flags & Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION) != 0;
        boolean canRead = (flags & Intent.FLAG_GRANT_READ_URI_PERMISSION) != 0;
        if (!canPersist || !canRead) {
            return;
        }
        try {
            getContentResolver().takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION);
        } catch (SecurityException ignored) {
        }
    }

    private void appendLog(String level, String message) {
        appendLog(level, message, true);
    }

    private void appendLog(String level, String message, boolean persistDiagnostics) {
        if (TrafficLogParser.isProgressLog(message)) {
            return;
        }
        if (persistDiagnostics) {
            GoncCrashReporter.appendLog(this, level, message);
        }
        String cleanLevel = level == null || level.trim().isEmpty() ? "info" : level;
        logs.add(cleanLevel.toUpperCase(Locale.ROOT) + "  " + message);
        while (logs.size() > MAX_ACTIVITY_LOGS) {
            logs.remove(0);
        }
    }

    // --- UI DSL delegates to the shared UiKit (transitional; removed as the
    // panel view code moves into per-module classes that call ui.* directly). ---

    private Button modeButton(String label, boolean active) {
        return ui.modeButton(label, active);
    }

    private Button primaryButton(String label) {
        return ui.primaryButton(label);
    }

    private Button outlineButton(String label) {
        return ui.outlineButton(label);
    }

    private Button dangerButton(String label) {
        return ui.dangerButton(label);
    }

    private Button warningButton(String label) {
        return ui.warningButton(label);
    }

    private Button secondaryButton(String label) {
        return ui.secondaryButton(label);
    }

    private Button ghostButton(String label) {
        return ui.ghostButton(label);
    }

    private Button compactGhostButton(String label) {
        return ui.compactGhostButton(label);
    }

    private Button quietTouchButton(String label) {
        return ui.quietTouchButton(label);
    }

    private Button button(String label, int color, int background) {
        return ui.button(label, color, background);
    }

    private void setControlEnabled(View view, boolean enabled) {
        ui.setControlEnabled(view, enabled);
    }

    private TextView text(String value, int sp, int color, int style) {
        return ui.text(value, sp, color, style);
    }

    private LinearLayout card() {
        return ui.card();
    }

    private LinearLayout row() {
        return ui.row();
    }

    private LinearLayout column() {
        return ui.column();
    }

    private LinearLayout.LayoutParams blockParams() {
        return ui.blockParams();
    }

    private LinearLayout.LayoutParams blockParams(int topMargin) {
        return ui.blockParams(topMargin);
    }

    private LinearLayout.LayoutParams dividerParams(int topMargin) {
        return ui.dividerParams(topMargin);
    }

    private LinearLayout.LayoutParams actionParams() {
        return ui.actionParams();
    }

    private LinearLayout.LayoutParams actionParams(int height) {
        return ui.actionParams(height);
    }

    private LinearLayout.LayoutParams metricParams() {
        return ui.metricParams();
    }

    private GradientDrawable rounded(int color, int radius, int strokeColor, int strokeWidthDp) {
        return ui.rounded(color, radius, strokeColor, strokeWidthDp);
    }

    private int dp(int value) {
        return ui.dp(value);
    }

    private int ink() {
        return ui.ink();
    }

    private int muted() {
        return ui.muted();
    }

    private String formatBytes(long value) {
        return ui.formatBytes(value);
    }

    private String formatRate(double bytesPerSecond) {
        return ui.formatRate(bytesPerSecond);
    }

    private String formatPercent(long done, long total) {
        return ui.formatPercent(done, total);
    }

    private int progressValue(long done, long total) {
        return ui.progressValue(done, total);
    }

    private String formatDuration(long seconds) {
        return ui.formatDuration(seconds);
    }

    private String emptyDash(String value) {
        return ui.emptyDash(value);
    }

    private String titleCase(String value) {
        return ui.titleCase(value);
    }
}
