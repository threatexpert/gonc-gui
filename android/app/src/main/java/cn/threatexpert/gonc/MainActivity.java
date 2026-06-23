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
import android.view.WindowManager;
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
    private static final int MAX_ACTIVITY_LOGS = 500;
    private static final int MAX_VISIBLE_ACTIVITY_LOGS = 80;
    private static final String PREFS = "gonc_main";
    private static final String KEY_VPN_PROFILES = "vpn_profiles";
    private static final String KEY_SELECTED_VPN_PROFILE = "selected_vpn_profile";
    private static final String VPN_PROFILE_QR_TYPE = "gonc.vpn.profile";

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final UiKit ui = new UiKit(this);
    private final List<String> logs = new ArrayList<>();
    private final GoncBridge bridge = new MobileGoncBridge();
    private final TransferMetrics receiveMetrics = new TransferMetrics();
    private VpnServerController vpnServer;
    private VpnClientController vpnClient;
    private SendController sendController;
    private java.util.function.Consumer<String> pendingScanCallback;
    private boolean pendingScanIsProfile;
    private final Object downloadProgressLock = new Object();

    private LinearLayout root;
    private boolean sendMode = true;
    private boolean vpnMode;
    private boolean vpnServerMode;
    private boolean receiveUseUdp;
    private boolean pendingVpnStartAfterBatterySettings;
    private boolean activityExpanded;
    private boolean receivePasswordVisible;
    private int receivePasswordVisibilityToken;
    private String receivePassword = "";
    private Uri saveTreeUri;
    private String saveLocationLabel;
    private String receiveEndpoint = "";
    private String remoteListStatus = "Idle";
    private String remoteCurrentPath = "";
    private boolean remoteCurrentPathMissing;
    private final List<HttpReceiver.RemoteFile> remoteFiles = new ArrayList<>();
    private final Set<String> selectedRemotePaths = new HashSet<>();
    private int remoteFileCount;
    private int remoteDirCount;
    private long remoteTotalBytes;
    private boolean resumeDownloads = true;
    private String downloadStatus = "Idle";
    private int downloadDoneFiles;
    private int downloadTotalFiles;
    private long downloadDoneBytes;
    private long downloadTotalBytes;
    private long downloadNetworkBytes;
    private double downloadBytesPerSecond;
    private int downloadSkippedFiles;
    private int downloadResumedFiles;
    private long downloadStartedAtMs;
    private long downloadFinishedAtMs;
    private String receiveStatus = "Idle";
    private GoncBridge.Session receiveSession;
    private HttpReceiver.Session remoteListSession;
    private HttpReceiver.Session receiveDownload;
    private long receiveRunId;
    private long pauseAutoRenderUntilMs;
    private long lastLogRenderMs;
    private long lastDownloadProgressRenderMs;
    private boolean downloadProgressRenderPending;
    private boolean downloadProgressApplyPending;
    private DownloadProgressSnapshot pendingDownloadProgress;
    private TextView downloadStatusView;
    private TextView downloadDetailView;
    private ProgressBar downloadProgressBar;
    private TextView downloadSummaryView;
    private View receiveConnectionDotView;
    private TextView receiveConnectionLabelView;
    private TextView activitySummaryTextView;
    private LinearLayout activityLogContentView;
    private TextView activityP2PStatusValueView;
    private TextView activitySpeedValueView;
    private TextView activityConnectionsValueView;
    private TextView activityNetworkValueView;
    private TextView activityRouteValueView;
    private TextView activityPeerValueView;
    private TextView activityEndpointValueView;
    private TextView remoteFilesSummaryTextView;
    private TextView remoteFilesTotalSummaryTextView;
    private Button downloadSelectedButton;
    private final Map<String, CheckBox> remoteFileCheckboxes = new LinkedHashMap<>();
    private boolean updatingRemoteSelectionViews;

    private static final int METRIC_REF_NONE = 0;
    private static final int METRIC_REF_P2P_STATUS = 1;
    private static final int METRIC_REF_SPEED = 2;
    private static final int METRIC_REF_CONNECTIONS = 3;
    private static final int METRIC_REF_NETWORK = 4;
    private static final int METRIC_REF_PEER = 5;
    private static final int METRIC_REF_ENDPOINT = 6;
    private static final int METRIC_REF_ROUTE = 7;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        GoncCrashReporter.install(this);
        saveLocationLabel = getString(R.string.default_save_location);
        RetainedModules retained = (RetainedModules) getLastNonConfigurationInstance();
        boolean reusedModules = retained != null;
        if (reusedModules) {
            vpnServer = retained.vpnServer;
            vpnClient = retained.vpnClient;
            sendController = retained.sendController;
            vpnServer.attach(this);
            vpnClient.attach(this);
            sendController.attach(this);
        } else {
            vpnServer = new VpnServerController(this);
            vpnClient = new VpnClientController(this);
            sendController = new SendController(this);
            vpnClient.load();
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
            saveTreeUri = data.getData();
            int flags = data.getFlags() & (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            try {
                if (flags != 0) {
                    getContentResolver().takePersistableUriPermission(saveTreeUri, flags);
                }
            } catch (RuntimeException ignored) {
            }
            saveLocationLabel = HttpReceiver.displayName(this, saveTreeUri);
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
        }
    }

    @Override
    public Object onRetainNonConfigurationInstance() {
        // Keep the extracted module controllers (and their live sessions)
        // alive across a configuration-change recreation; the new Activity
        // re-attaches itself via attach(). Receive state is not retained yet.
        return new RetainedModules(vpnServer, vpnClient, sendController);
    }

    @Override
    protected void onDestroy() {
        // On a config change the controllers are retained, so leave them and
        // their sessions running; only tear down on a real destroy.
        if (!isChangingConfigurations()) {
            vpnClient.unregister();
            sendController.shutdown();
            vpnServer.shutdown();
        }
        if (receiveSession != null) {
            receiveSession.stop();
        }
        if (remoteListSession != null) {
            remoteListSession.stop();
        }
        if (receiveDownload != null) {
            receiveDownload.stop();
        }
        super.onDestroy();
    }

    private static final class RetainedModules {
        final VpnServerController vpnServer;
        final VpnClientController vpnClient;
        final SendController sendController;

        RetainedModules(VpnServerController vpnServer, VpnClientController vpnClient, SendController sendController) {
            this.vpnServer = vpnServer;
            this.vpnClient = vpnClient;
            this.sendController = sendController;
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
        scrollView.setFitsSystemWindows(true);

        root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setFitsSystemWindows(true);
        root.setPadding(dp(18), dp(22), dp(18), dp(24));
        scrollView.addView(root, new ScrollView.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
        ));
        setContentView(scrollView);
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
            root.addView(receivePanel());
            if (!receiveEndpoint.trim().isEmpty() || !remoteFiles.isEmpty() || !"Idle".equals(remoteListStatus)) {
                root.addView(remoteFilesPanel());
            }
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
        return !vpnMode && !vpnServerMode && !sendMode && (!receiveEndpoint.trim().isEmpty() || !remoteFiles.isEmpty() || !"Idle".equals(remoteListStatus));
    }

    private void updateBackgroundDynamicViews() {
        if (receiveConnectionDotView != null) {
            receiveConnectionDotView.setBackground(rounded(receiveConnectionColor(), dp(6), 0, 0));
        }
        if (receiveConnectionLabelView != null) {
            receiveConnectionLabelView.setText(receiveConnectionLabel());
        }
        if (activitySummaryTextView != null) {
            activitySummaryTextView.setText(activitySummary(currentMetrics()));
        }
        updateActivityMetricViews();
        updateActivityLogViews();
    }

    private void renderDownloadProgress() {
        if (System.currentTimeMillis() < pauseAutoRenderUntilMs) {
            return;
        }
        long now = System.currentTimeMillis();
        long elapsed = now - lastDownloadProgressRenderMs;
        if (elapsed >= 350) {
            lastDownloadProgressRenderMs = now;
            updateDownloadProgressViews();
            return;
        }
        if (downloadProgressRenderPending) {
            return;
        }
        downloadProgressRenderPending = true;
        mainHandler.postDelayed(() -> {
            downloadProgressRenderPending = false;
            if (receiveDownload == null || System.currentTimeMillis() < pauseAutoRenderUntilMs) {
                return;
            }
            lastDownloadProgressRenderMs = System.currentTimeMillis();
            updateDownloadProgressViews();
        }, Math.max(50, 350 - elapsed));
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

        if (isAnySessionRunning()) {
            View keepAwake = keepScreenIndicator();
            LinearLayout.LayoutParams indicatorParams = new LinearLayout.LayoutParams(dp(38), dp(38));
            indicatorParams.setMargins(0, 0, dp(6), 0);
            header.addView(keepAwake, indicatorParams);
        }

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

    private View keepScreenIndicator() {
        ImageView icon = new ImageView(this);
        icon.setImageResource(R.drawable.ic_keep_screen_on);
        icon.setPadding(dp(7), dp(7), dp(7), dp(7));
        icon.setBackground(rounded(Color.rgb(223, 243, 236), dp(8), Color.rgb(143, 211, 189), 1));
        icon.setContentDescription(getString(R.string.screen_stays_on));
        icon.setOnClickListener(v -> Toast.makeText(this, R.string.screen_stays_on, Toast.LENGTH_SHORT).show());
        return icon;
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

    private View receivePanel() {
        LinearLayout card = card();
        if (receiveSession != null) {
            card.addView(receiveSessionBarView());
            return card;
        }
        card.addView(sectionBoundaryTitle(getString(R.string.save_location_config), false), blockParams(0));
        LinearLayout save = row();
        TextView saveText = text(getString(R.string.save_to, saveLocationLabel), 14, Color.rgb(38, 56, 79), Typeface.BOLD);
        saveText.setSingleLine(true);
        save.addView(saveText, new LinearLayout.LayoutParams(0, dp(42), 1));
        Button choose = secondaryButton(getString(R.string.choose));
        choose.setOnClickListener(v -> openSaveLocationPicker());
        LinearLayout.LayoutParams chooseParams = new LinearLayout.LayoutParams(dp(104), dp(42));
        chooseParams.setMargins(dp(8), 0, 0, 0);
        save.addView(choose, chooseParams);
        card.addView(save, blockParams());
        card.addView(sectionBoundaryTitle(getString(R.string.passphrase_config), true), blockParams(dp(14)));
        card.addView(passwordField());
        card.addView(sectionDivider(), dividerParams(dp(12)));
        card.addView(protocolToggle());

        Button primary = receiveSession == null ? primaryButton(getString(R.string.connect)) : dangerButton(getString(R.string.disconnect));
        primary.setOnClickListener(v -> {
            if (receiveSession == null) {
                startP2PReceive();
            } else {
                stopSession();
            }
        });
        card.addView(primary, blockParams(dp(12)));
        return card;
    }

    private View receiveSessionBarView() {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(10), dp(8), dp(10), dp(8));
        row.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(226, 232, 240), 1));

        View dot = new View(this);
        dot.setBackground(rounded(receiveConnectionColor(), dp(6), 0, 0));
        receiveConnectionDotView = dot;
        row.addView(dot, new LinearLayout.LayoutParams(dp(12), dp(12)));

        TextView label = text(receiveConnectionLabel(), 14, ink(), Typeface.BOLD);
        label.setSingleLine(false);
        receiveConnectionLabelView = label;
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        labelParams.setMargins(dp(10), 0, dp(8), 0);
        row.addView(label, labelParams);

        Button passphrase = secondaryButton(getString(R.string.passphrase));
        passphrase.setOnClickListener(v -> showPasswordQr());
        row.addView(passphrase, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));

        Button disconnect = dangerButton(getString(R.string.disconnect));
        disconnect.setTextSize(14);
        disconnect.setOnClickListener(v -> stopSession());
        LinearLayout.LayoutParams disconnectParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38));
        disconnectParams.setMargins(dp(8), 0, 0, 0);
        row.addView(disconnect, disconnectParams);
        return row;
    }

    private View receiveProgressPanel() {
        LinearLayout card = card();
        card.addView(sectionTitle(getString(R.string.receive_status)));
        card.addView(receiveProgressContent());
        return card;
    }

    private View receiveProgressContent() {
        LinearLayout box = column();
        TextView line = text(displayDownloadStatus(downloadStatus), 14, ink(), Typeface.BOLD);
        line.setSingleLine(false);
        downloadStatusView = line;
        box.addView(line);

        long total = Math.max(downloadTotalBytes, 0);
        long done = Math.max(downloadDoneBytes, 0);
        TextView detail = text(downloadProgressDetail(done, total), 13, muted(), Typeface.BOLD);
        detail.setSingleLine(false);
        downloadDetailView = detail;
        box.addView(detail, blockParams(dp(8)));

        ProgressBar progress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        progress.setMax(1000);
        progress.setProgress(progressValue(done, total));
        downloadProgressBar = progress;
        box.addView(progress, blockParams(dp(6)));

        TextView speed = text(downloadProgressSummary(done, total), 13, Color.rgb(15, 111, 83), Typeface.BOLD);
        downloadSummaryView = speed;
        box.addView(speed, blockParams(dp(4)));
        return box;
    }

    private View remoteFilesPanel() {
        LinearLayout card = card();
        remoteFileCheckboxes.clear();
        card.addView(sectionTitle(getString(R.string.remote_files)));

        LinearLayout pathRow = row();
        pathRow.setGravity(Gravity.CENTER_VERTICAL);
        pathRow.addView(remoteBreadcrumbView(), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button selectAll = quietTouchButton(getString(R.string.select_all));
        setControlEnabled(selectAll, canClickRemoteAction() && !visibleRemoteFiles().isEmpty());
        selectAll.setOnClickListener(v -> selectVisibleRemoteFiles());
        pathRow.addView(selectAll, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(44)));
        Button invert = quietTouchButton(getString(R.string.invert_selection));
        setControlEnabled(invert, canClickRemoteAction() && !visibleRemoteFiles().isEmpty());
        invert.setOnClickListener(v -> invertVisibleRemoteFiles());
        pathRow.addView(invert, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(44)));
        Button refresh = quietTouchButton(getString(R.string.refresh));
        setControlEnabled(refresh, canClickRemoteAction());
        refresh.setOnClickListener(v -> refreshRemoteFiles(remoteCurrentPath));
        pathRow.addView(refresh, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(44)));
        card.addView(pathRow, blockParams(dp(8)));
        if (shouldShowRemoteListStatus()) {
            card.addView(remoteListStatusView(), blockParams(dp(4)));
        }

        boolean currentRoot = normalizeRemotePath(remoteCurrentPath).isEmpty();
        if (remoteFiles.isEmpty() && currentRoot && !remoteCurrentPathMissing) {
            card.addView(text(getString(R.string.remote_list_waiting), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
            return card;
        }

        List<HttpReceiver.RemoteFile> visible = visibleRemoteFiles();
        LinearLayout list = column();
        remoteFileCheckboxes.clear();
        if (!remoteCurrentPath.isEmpty()) {
            list.addView(parentDirectoryRow());
        }
        if (remoteCurrentPathMissing) {
            list.addView(text(getString(R.string.folder_missing), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
        } else if (visible.isEmpty()) {
            list.addView(text(currentRoot ? getString(R.string.toast_no_remote_files) : getString(R.string.folder_empty), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
        } else {
            for (HttpReceiver.RemoteFile file : visible) {
                list.addView(remoteFileRow(file));
            }
        }
        ScrollView listScroll = new InterceptingScrollView(this);
        listScroll.setFillViewport(false);
        listScroll.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(226, 232, 240), 1));
        listScroll.addView(list, new ScrollView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        int rowCount = visible.size() + (remoteCurrentPath.isEmpty() ? 0 : 1) + (visible.isEmpty() || remoteCurrentPathMissing ? 1 : 0);
        int listHeight = Math.min(dp(340), Math.max(dp(96), rowCount * dp(58) + dp(16)));
        LinearLayout.LayoutParams listParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, listHeight);
        listParams.setMargins(0, dp(8), 0, 0);
        card.addView(listScroll, listParams);
        card.addView(remoteFilesSummaryView(), blockParams(dp(10)));
        if (shouldShowDownloadProgress()) {
            card.addView(receiveProgressContent(), blockParams(dp(8)));
        }
        card.addView(remoteDownloadActionBar(), blockParams(dp(10)));
        return card;
    }

    private View remoteDownloadActionBar() {
        LinearLayout box = column();
        box.setPadding(dp(10), dp(10), dp(10), dp(10));
        box.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(226, 232, 240), 1));
        box.addView(downloadModeToggle());

        LinearLayout actions = row();
        if (receiveDownload != null) {
            Button stop = warningButton(getString(R.string.stop_download));
            stop.setOnClickListener(v -> stopReceiveDownload());
            stop.setOnTouchListener((v, event) -> {
                if (event.getAction() == MotionEvent.ACTION_DOWN) {
                    stopReceiveDownload();
                    return true;
                }
                return false;
            });
            actions.addView(stop, new LinearLayout.LayoutParams(0, dp(42), 1));
        } else {
            Button selected = outlineButton(getString(R.string.download_selected));
            downloadSelectedButton = selected;
            boolean canDownloadSelected = canStartRemoteDownload() && !selectedRemotePaths.isEmpty();
            setControlEnabled(selected, canDownloadSelected);
            selected.setOnClickListener(v -> refreshAndStartDownload(new ArrayList<>(selectedRemotePaths)));

            Button all = primaryButton(getString(R.string.receive_all));
            setControlEnabled(all, canStartRemoteDownload());
            all.setOnClickListener(v -> refreshAndStartDownload(currentDownloadPaths()));

            actions.addView(selected, new LinearLayout.LayoutParams(0, dp(42), 1));
            actions.addView(all, actionParams(dp(42)));
        }
        box.addView(actions, blockParams(dp(10)));
        return box;
    }

    private View parentDirectoryRow() {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(12), dp(10), dp(12), dp(10));
        row.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));

        TextView icon = text("\u21b0", 20, Color.rgb(32, 101, 165), Typeface.BOLD);
        icon.setGravity(Gravity.CENTER);
        row.addView(icon, new LinearLayout.LayoutParams(dp(34), dp(42)));

        LinearLayout labels = column();
        TextView name = text(getString(R.string.parent_directory), 13, Color.rgb(32, 101, 165), Typeface.BOLD);
        labels.addView(name);
        labels.addView(text(parentPath(remoteCurrentPath).isEmpty() ? "/" : displayRemotePath(parentPath(remoteCurrentPath)), 12, muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row.addView(text("\u203a", 22, muted(), Typeface.BOLD), new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        row.setLayoutParams(blockParams(dp(6)));
        setControlEnabled(row, canClickRemoteAction());
        row.setOnClickListener(v -> {
            if (canClickRemoteAction()) {
                browseRemotePath(parentPath(remoteCurrentPath));
            }
        });
        return row;
    }

    private View remoteFileRow(HttpReceiver.RemoteFile file) {
        String normalizedPath = normalizeRemotePath(file.path);
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(10), dp(6), dp(10), dp(6));
        row.setBackground(rounded(file.isDir ? Color.rgb(246, 250, 255) : Color.WHITE, dp(7), Color.rgb(226, 232, 240), 1));

        CheckBox checkBox = new CheckBox(this);
        checkBox.setChecked(selectedRemotePaths.contains(normalizedPath));
        remoteFileCheckboxes.put(normalizedPath, checkBox);
        setControlEnabled(checkBox, canClickRemoteAction());
        checkBox.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (updatingRemoteSelectionViews) {
                return;
            }
            if (!canClickRemoteAction()) {
                return;
            }
            if (isChecked) {
                selectedRemotePaths.add(normalizedPath);
            } else {
                selectedRemotePaths.remove(normalizedPath);
            }
            updateRemoteSelectionViews();
        });
        row.addView(checkBox, new LinearLayout.LayoutParams(dp(42), dp(42)));

        TextView icon = text(file.isDir ? "\uD83D\uDCC1" : "\uD83D\uDCC4", 20, file.isDir ? Color.rgb(32, 101, 165) : muted(), Typeface.NORMAL);
        icon.setGravity(Gravity.CENTER);
        row.addView(icon, new LinearLayout.LayoutParams(dp(34), dp(42)));

        LinearLayout labels = column();
        TextView name = text(baseName(normalizedPath), 13, file.isDir ? Color.rgb(32, 101, 165) : ink(), Typeface.BOLD);
        name.setSingleLine(true);
        labels.addView(name);
        labels.addView(text(file.isDir ? getString(R.string.folder) : formatBytes(file.size), 12, muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        if (file.isDir) {
            row.addView(text("\u203a", 22, Color.rgb(32, 101, 165), Typeface.BOLD), new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        }
        row.setLayoutParams(blockParams(dp(6)));
        setControlEnabled(row, canClickRemoteAction());
        row.setOnClickListener(v -> {
            if (file.isDir && canClickRemoteAction()) {
                browseRemotePath(normalizedPath);
            }
        });
        return row;
    }

    private View remoteBreadcrumbView() {
        LinearLayout crumbs = row();
        crumbs.setGravity(Gravity.CENTER_VERTICAL);
        String current = normalizeRemotePath(remoteCurrentPath);
        addBreadcrumbPart(crumbs, "/", "");
        if (!current.isEmpty()) {
            StringBuilder path = new StringBuilder();
            for (String part : current.split("/")) {
                if (part.isEmpty()) {
                    continue;
                }
                crumbs.addView(text("\u203a", 16, muted(), Typeface.BOLD));
                if (path.length() > 0) {
                    path.append('/');
                }
                path.append(part);
                addBreadcrumbPart(crumbs, part, path.toString());
            }
        }
        return crumbs;
    }

    private void addBreadcrumbPart(LinearLayout row, String label, String path) {
        TextView part = text(label, 13, Color.rgb(32, 101, 165), Typeface.BOLD);
        part.setSingleLine(true);
        part.setPadding(dp(6), dp(6), dp(6), dp(6));
        part.setBackground(rounded(Color.TRANSPARENT, dp(6), 0, 0));
        setControlEnabled(part, canClickRemoteAction());
        part.setOnClickListener(v -> {
            if (canClickRemoteAction()) {
                browseRemotePath(path);
            }
        });
        row.addView(part, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT));
    }

    private View receiveConnectionStatusView() {
        LinearLayout box = row();
        box.setGravity(Gravity.CENTER_VERTICAL);
        box.setPadding(dp(10), dp(8), dp(10), dp(8));
        box.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(226, 232, 240), 1));

        View dot = new View(this);
        dot.setBackground(rounded(receiveConnectionColor(), dp(6), 0, 0));
        receiveConnectionDotView = dot;
        box.addView(dot, new LinearLayout.LayoutParams(dp(12), dp(12)));

        LinearLayout labels = column();
        TextView label = text(receiveConnectionLabel(), 14, ink(), Typeface.BOLD);
        receiveConnectionLabelView = label;
        labels.addView(label);
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        labelParams.setMargins(dp(10), 0, 0, 0);
        box.addView(labels, labelParams);
        return box;
    }

    private String receiveConnectionLabel() {
        if (receiveSession == null) {
            return getString(R.string.connection_disconnected);
        }
        String state = receiveConnectionState();
        String route = receiveRouteLabel();
        if ("failed".equals(state)) {
            return getString(R.string.connection_failed_retry);
        }
        if ("connected".equals(state)) {
            return appendRoute(getString(R.string.connection_connected), route);
        }
        if ("negotiating".equals(state)) {
            return appendRoute(getString(R.string.connection_negotiating), route);
        }
        if ("waiting".equals(state)) {
            return getString(R.string.connection_waiting_peer);
        }
        return getString(R.string.connection_connecting);
    }

    private String appendRoute(String label, String route) {
        return ui.appendRoute(label, route);
    }

    private String receiveRouteLabel() {
        return routeLabel(receiveMetrics.routeMode);
    }

    private String routeLabel(String mode) {
        return ui.routeLabel(mode);
    }

    private String displayRouteMetric(TransferMetrics metrics) {
        String route = routeLabel(metrics.routeMode);
        return route.isEmpty() ? "-" : route;
    }

    private int receiveConnectionColor() {
        if (receiveSession == null) {
            return Color.rgb(148, 163, 184);
        }
        String state = receiveConnectionState();
        if ("failed".equals(state)) {
            return Color.rgb(201, 63, 63);
        }
        if ("connected".equals(state)) {
            return Color.rgb(18, 151, 101);
        }
        return Color.rgb(222, 153, 42);
    }

    private String receiveConnectionState() {
        if (receiveSession == null) {
            return "disconnected";
        }
        String status = displayStatus(receiveMetrics);
        if (status == null) {
            return "waiting";
        }
        String clean = status.trim().toLowerCase(Locale.ROOT);
        if ("failed".equals(clean) || "error".equals(clean)) {
            return "failed";
        }
        if ("connected".equals(clean)) {
            return "connected";
        }
        if ("negotiating".equals(clean)) {
            return "negotiating";
        }
        if ("wait".equals(clean)
                || "waiting".equals(clean)
                || "ready".equals(clean)
                || "idle".equals(clean)
                || "disconnected".equals(clean)) {
            return "waiting";
        }
        return "connecting";
    }

    private View remoteFilesSummaryView() {
        LinearLayout box = column();
        TextView selected = text(selectionSummaryText(), 14, ink(), Typeface.BOLD);
        selected.setSingleLine(false);
        remoteFilesSummaryTextView = selected;
        box.addView(selected);

        TextView total = text(remoteTotalSummaryText(), 12, muted(), Typeface.NORMAL);
        total.setSingleLine(false);
        remoteFilesTotalSummaryTextView = total;
        box.addView(total, blockParams(dp(3)));
        return box;
    }

    private void updateRemoteSelectionViews() {
        updatingRemoteSelectionViews = true;
        try {
            for (Map.Entry<String, CheckBox> entry : remoteFileCheckboxes.entrySet()) {
                entry.getValue().setChecked(selectedRemotePaths.contains(entry.getKey()));
            }
        } finally {
            updatingRemoteSelectionViews = false;
        }
        if (remoteFilesSummaryTextView != null) {
            remoteFilesSummaryTextView.setText(selectionSummaryText());
        }
        if (remoteFilesTotalSummaryTextView != null) {
            remoteFilesTotalSummaryTextView.setText(remoteTotalSummaryText());
        }
        if (downloadSelectedButton != null) {
            setControlEnabled(downloadSelectedButton, canStartRemoteDownload() && !selectedRemotePaths.isEmpty());
        }
    }

    private String selectionSummaryText() {
        SelectionSummary selected = selectedRemoteSummary();
        return getString(R.string.selection_summary, selectedRemotePaths.size(), formatBytes(selected.bytes));
    }

    private String remoteTotalSummaryText() {
        return getString(R.string.remote_total_summary, remoteFileCount, remoteDirCount, formatBytes(remoteTotalBytes));
    }

    private SelectionSummary selectedRemoteSummary() {
        SelectionSummary summary = new SelectionSummary();
        for (HttpReceiver.RemoteFile file : selectedRemoteFiles()) {
            if (!file.isDir) {
                summary.files++;
                summary.bytes += Math.max(0, file.size);
            }
        }
        return summary;
    }

    private boolean canStartRemoteDownload() {
        return canClickRemoteAction()
                && "connected".equals(receiveConnectionState())
                && !remoteCurrentPathMissing
                && !currentDirectoryFiles().isEmpty();
    }

    private boolean shouldShowDownloadProgress() {
        return receiveDownload != null
                || "Receive complete".equals(downloadStatus)
                || "Receive failed".equals(downloadStatus);
    }

    private boolean shouldShowRemoteListStatus() {
        if (isRemoteListBusy()) {
            return true;
        }
        String status = remoteListStatus == null ? "" : remoteListStatus.trim();
        return "Remote list failed".equals(status)
                || "Remote refresh failed".equals(status)
                || "Failed".equals(status)
                || "Remote path missing".equals(status);
    }

    private View remoteListStatusView() {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        if (isRemoteListBusy()) {
            ProgressBar spinner = new ProgressBar(this, null, android.R.attr.progressBarStyleSmall);
            spinner.setIndeterminate(true);
            LinearLayout.LayoutParams spinnerParams = new LinearLayout.LayoutParams(dp(22), dp(22));
            spinnerParams.setMargins(0, 0, dp(8), 0);
            row.addView(spinner, spinnerParams);
        }
        TextView view = text(displayRemoteListStatus(), 12, remoteListStatusColor(), Typeface.BOLD);
        view.setSingleLine(false);
        row.addView(view, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        return row;
    }

    private boolean isRemoteListBusy() {
        String status = remoteListStatus == null ? "" : remoteListStatus.trim();
        return status.startsWith("Fetching ") || "Refreshing remote file list".equals(status);
    }

    private String displayRemoteListStatus() {
        String status = remoteListStatus == null ? "" : remoteListStatus.trim();
        if (status.isEmpty() || "Idle".equals(status)) {
            return getString(R.string.status_idle);
        }
        if ("Waiting for peer".equals(status)) {
            return getString(R.string.connection_waiting_peer);
        }
        if (status.startsWith("Fetching ")) {
            return getString(R.string.remote_list_fetching, status.substring("Fetching ".length()));
        }
        if ("Refreshing remote file list".equals(status)) {
            return getString(R.string.remote_list_refreshing);
        }
        if ("Remote list ready".equals(status)) {
            return getString(R.string.remote_list_ready_status);
        }
        if ("Remote list refreshed".equals(status)) {
            return getString(R.string.remote_list_refreshed_status);
        }
        if ("No remote files".equals(status)) {
            return getString(R.string.toast_no_remote_files);
        }
        if ("Remote path missing".equals(status)) {
            return getString(R.string.folder_missing);
        }
        if ("Remote list failed".equals(status) || "Remote refresh failed".equals(status) || "Failed".equals(status)) {
            return getString(R.string.remote_list_failed_status);
        }
        if ("Stopped".equals(status)) {
            return getString(R.string.status_stopped);
        }
        return status;
    }

    private int remoteListStatusColor() {
        String status = remoteListStatus == null ? "" : remoteListStatus.trim();
        if ("Remote list ready".equals(status) || "Remote list refreshed".equals(status)) {
            return Color.rgb(15, 111, 83);
        }
        if ("Remote list failed".equals(status) || "Remote refresh failed".equals(status) || "Failed".equals(status) || "Remote path missing".equals(status)) {
            return Color.rgb(201, 63, 63);
        }
        if ("Waiting for peer".equals(status) || status.startsWith("Fetching ") || "Refreshing remote file list".equals(status)) {
            return Color.rgb(172, 103, 22);
        }
        return muted();
    }

    private String displayDownloadStatus(String status) {
        if ("Idle".equals(status)) {
            return getString(R.string.status_idle);
        }
        if ("Preparing download".equals(status)) {
            return getString(R.string.status_preparing_download);
        }
        if ("Receive complete".equals(status)) {
            return getString(R.string.status_receive_complete);
        }
        if ("Receive failed".equals(status)) {
            return getString(R.string.status_receive_failed);
        }
        if ("Stopped".equals(status)) {
            return getString(R.string.status_stopped);
        }
        if (status != null && status.startsWith("Receiving ")) {
            return getString(R.string.status_receiving, status.substring("Receiving ".length()));
        }
        return status == null ? "" : status;
    }

    private double currentDownloadSpeed() {
        return receiveDownload == null ? 0 : downloadBytesPerSecond;
    }

    private View downloadModeToggle() {
        LinearLayout box = column();
        LinearLayout line = row();
        line.setGravity(Gravity.CENTER_VERTICAL);
        TextView label = text(getString(R.string.mode), 12, muted(), Typeface.BOLD);
        line.addView(label, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(30)));
        Button resume = segmentedButton(getString(R.string.resume), resumeDownloads);
        setControlEnabled(resume, receiveDownload == null);
        resume.setOnClickListener(v -> {
            if (receiveDownload == null) {
                resumeDownloads = true;
                render();
            }
        });
        Button overwrite = segmentedButton(getString(R.string.overwrite), !resumeDownloads);
        setControlEnabled(overwrite, receiveDownload == null);
        overwrite.setOnClickListener(v -> {
            if (receiveDownload == null) {
                resumeDownloads = false;
                render();
            }
        });
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(30), 1);
        params.setMargins(dp(8), 0, 0, 0);
        line.addView(resume, params);
        line.addView(overwrite, actionParams(dp(30)));
        box.addView(line);
        box.addView(text(getString(R.string.resume_overwrite_hint), 12, muted(), Typeface.NORMAL), blockParams(dp(4)));
        return box;
    }

    private Button segmentedButton(String label, boolean active) {
        return ui.segmentedButton(label, active);
    }

    private boolean canClickRemoteAction() {
        return receiveDownload == null && remoteListSession == null;
    }

    private void browseRemotePath(String path) {
        if (!canClickRemoteAction()) {
            return;
        }
        remoteCurrentPath = normalizeRemotePath(path);
        remoteListStatus = remoteFiles.isEmpty() ? "No remote files" : "Remote list ready";
        render();
    }

    private List<String> currentDownloadPaths() {
        List<String> paths = new ArrayList<>();
        paths.add(normalizeRemotePath(remoteCurrentPath));
        return paths;
    }

    private List<HttpReceiver.RemoteFile> selectedRemoteFiles() {
        List<HttpReceiver.RemoteFile> files = new ArrayList<>();
        for (HttpReceiver.RemoteFile file : remoteFiles) {
            if (isSelectedForDownload(file)) {
                files.add(file);
            }
        }
        return files;
    }

    private boolean isSelectedForDownload(HttpReceiver.RemoteFile file) {
        String path = normalizeRemotePath(file.path);
        if (selectedRemotePaths.contains(path)) {
            return true;
        }
        for (String selected : selectedRemotePaths) {
            if (!path.isEmpty() && selected != null && path.startsWith(selected + "/")) {
                return true;
            }
        }
        return false;
    }

    private void selectVisibleRemoteFiles() {
        for (HttpReceiver.RemoteFile file : visibleRemoteFiles()) {
            selectedRemotePaths.add(normalizeRemotePath(file.path));
        }
        updateRemoteSelectionViews();
    }

    private void invertVisibleRemoteFiles() {
        for (HttpReceiver.RemoteFile file : visibleRemoteFiles()) {
            String path = normalizeRemotePath(file.path);
            if (selectedRemotePaths.contains(path)) {
                selectedRemotePaths.remove(path);
            } else {
                selectedRemotePaths.add(path);
            }
        }
        updateRemoteSelectionViews();
    }

    private List<HttpReceiver.RemoteFile> currentDirectoryFiles() {
        List<HttpReceiver.RemoteFile> files = new ArrayList<>();
        String current = normalizeRemotePath(remoteCurrentPath);
        for (HttpReceiver.RemoteFile file : remoteFiles) {
            String path = normalizeRemotePath(file.path);
            if (current.isEmpty() || path.equals(current) || path.startsWith(current + "/")) {
                files.add(file);
            }
        }
        return files;
    }

    private List<HttpReceiver.RemoteFile> visibleRemoteFiles() {
        Map<String, HttpReceiver.RemoteFile> visible = new LinkedHashMap<>();
        String current = normalizeRemotePath(remoteCurrentPath);
        for (HttpReceiver.RemoteFile file : remoteFiles) {
            String path = normalizeRemotePath(file.path);
            if (path.isEmpty() || path.equals(current)) {
                continue;
            }
            if (parentPath(path).equals(current)) {
                visible.put(path, file);
                continue;
            }
            if (isDescendantOf(path, current)) {
                String childPath = firstChildPath(path, current);
                if (!visible.containsKey(childPath)) {
                    HttpReceiver.RemoteFile dir = new HttpReceiver.RemoteFile();
                    dir.name = baseName(childPath);
                    dir.path = childPath;
                    dir.isDir = true;
                    dir.size = 0;
                    visible.put(childPath, dir);
                }
            }
        }
        List<HttpReceiver.RemoteFile> sorted = new ArrayList<>(visible.values());
        Collections.sort(sorted, (left, right) -> {
            if (left.isDir != right.isDir) {
                return left.isDir ? -1 : 1;
            }
            return baseName(left.path).compareToIgnoreCase(baseName(right.path));
        });
        return sorted;
    }

    private boolean isDescendantOf(String path, String parent) {
        if (parent.isEmpty()) {
            return path.contains("/");
        }
        return path.startsWith(parent + "/") && path.substring(parent.length() + 1).contains("/");
    }

    private String firstChildPath(String path, String parent) {
        String rest = parent.isEmpty() ? path : path.substring(parent.length() + 1);
        int slash = rest.indexOf('/');
        String child = slash >= 0 ? rest.substring(0, slash) : rest;
        return parent.isEmpty() ? child : parent + "/" + child;
    }

    private String parentPath(String path) {
        String normalized = normalizeRemotePath(path);
        int slash = normalized.lastIndexOf('/');
        return slash <= 0 ? "" : normalized.substring(0, slash);
    }

    private String baseName(String path) {
        String normalized = normalizeRemotePath(path);
        if (normalized.isEmpty()) {
            return "/";
        }
        int slash = normalized.lastIndexOf('/');
        return slash >= 0 ? normalized.substring(slash + 1) : normalized;
    }

    private String displayRemotePath(String path) {
        String normalized = normalizeRemotePath(path);
        return normalized.isEmpty() ? "/" : "/" + normalized;
    }

    private String normalizeRemotePath(String path) {
        String clean = path == null ? "" : path.trim().replace('\\', '/');
        while (clean.startsWith("/")) {
            clean = clean.substring(1);
        }
        while (clean.endsWith("/") && clean.length() > 1) {
            clean = clean.substring(0, clean.length() - 1);
        }
        return ".".equals(clean) ? "" : clean;
    }

    private View passwordField() {
        boolean locked = receiveSession != null;
        LinearLayout box = column();
        box.addView(text(getString(R.string.passphrase_hint), 12, muted(), Typeface.NORMAL), blockParams(dp(4)));

        LinearLayout line = row();
        EditText input = new EditText(this);
        input.setSingleLine(true);
        input.setText(receivePassword);
        input.setTextColor(ink());
        input.setTextSize(15);
        input.setHint(getString(R.string.passphrase_input_hint));
        input.setHintTextColor(Color.rgb(148, 163, 184));
        input.setPadding(dp(12), 0, dp(12), 0);
        input.setBackground(rounded(Color.WHITE, dp(6), Color.rgb(203, 215, 230), 1));
        input.setEnabled(!locked);
        input.setTextColor(ink());
        applyPasswordVisibility(input);
        input.addTextChangedListener(new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
                if (receiveSession != null) {
                    return;
                }
                receivePassword = s.toString();
                revealPasswordTemporarily(input);
            }

            @Override
            public void afterTextChanged(Editable s) {
            }
        });
        line.addView(input, new LinearLayout.LayoutParams(0, dp(46), 1));

        box.addView(line, blockParams(dp(8)));

        LinearLayout actions = row();
        Button random = secondaryButton(getString(R.string.random_passphrase));
        setControlEnabled(random, !locked);
        random.setOnClickListener(v -> randomizePassword());
        Button paste = secondaryButton(getString(R.string.paste));
        setControlEnabled(paste, !locked);
        paste.setOnClickListener(v -> pastePassword());
        Button scan = secondaryButton(getString(R.string.scan));
        setControlEnabled(scan, !locked);
        scan.setOnClickListener(v -> scanPassword());
        Button qr = secondaryButton(getString(R.string.qr));
        qr.setOnClickListener(v -> showPasswordQr());
        actions.addView(random, new LinearLayout.LayoutParams(0, dp(40), 1));
        actions.addView(paste, actionParams());
        actions.addView(scan, actionParams());
        actions.addView(qr, actionParams());
        box.addView(actions, blockParams(dp(6)));
        return box;
    }

    private void applyPasswordVisibility(EditText input) {
        input.setTransformationMethod(receivePasswordVisible ? null : PasswordTransformationMethod.getInstance());
        input.setSelection(input.getText().length());
    }

    private void revealPasswordTemporarily() {
        revealPasswordTemporarily(null);
    }

    private void revealPasswordTemporarily(EditText input) {
        receivePasswordVisible = true;
        int token = ++receivePasswordVisibilityToken;
        if (input != null) {
            applyPasswordVisibility(input);
        }
        mainHandler.postDelayed(() -> {
            if (receivePasswordVisibilityToken != token) {
                return;
            }
            receivePasswordVisible = false;
            if (input != null && input.isAttachedToWindow()) {
                applyPasswordVisibility(input);
            } else {
                render();
            }
        }, 5000);
    }

    private void hidePassword() {
        receivePasswordVisible = false;
        receivePasswordVisibilityToken++;
    }

    private void randomizePassword() {
        if (receiveSession != null) {
            return;
        }
        receivePassword = Passwords.generate();
        revealPasswordTemporarily();
        appendLog("info", "Passphrase randomized");
        render();
    }

    private View protocolToggle() {
        LinearLayout box = column();
        CheckBox checkBox = new CheckBox(this);
        checkBox.setText(getString(R.string.use_udp_protocol));
        checkBox.setTextColor(Color.rgb(64, 81, 105));
        checkBox.setTextSize(14);
        checkBox.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        checkBox.setChecked(receiveUseUdp);
        setControlEnabled(checkBox, receiveSession == null);
        checkBox.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (receiveSession != null) {
                return;
            }
            receiveUseUdp = isChecked;
        });
        box.addView(checkBox);
        TextView hint = text(getString(R.string.use_udp_protocol_hint), 12, muted(), Typeface.NORMAL);
        hint.setPadding(dp(4), 0, 0, 0);
        box.addView(hint);
        return box;
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
        if (vpnMode && !GoncVpnState.endpoint().trim().isEmpty()) {
            box.addView(metricBox(getString(R.string.vpn_endpoint), GoncVpnState.endpoint(), METRIC_REF_ENDPOINT), blockParams(dp(8)));
        } else if (!receiveEndpoint.trim().isEmpty()) {
            box.addView(metricBox(getString(R.string.file_endpoint), receiveEndpoint, METRIC_REF_ENDPOINT), blockParams(dp(8)));
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
        if (activityEndpointValueView != null) {
            activityEndpointValueView.setText(receiveEndpoint);
        }
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

    private void startP2PReceive() {
        String passphrase = receivePassword.trim();
        if (passphrase.isEmpty()) {
            Toast.makeText(this, R.string.toast_passphrase_required, Toast.LENGTH_SHORT).show();
            return;
        }
        if (Passwords.isWeak(passphrase)) {
            Toast.makeText(this, R.string.toast_passphrase_weak, Toast.LENGTH_SHORT).show();
            return;
        }
        hidePassword();
        receiveMetrics.reset();
        remoteListStatus = "Waiting for peer";
        downloadStatus = "Idle";
        downloadDoneFiles = 0;
        downloadTotalFiles = 0;
        downloadDoneBytes = 0;
        downloadTotalBytes = 0;
        downloadNetworkBytes = 0;
        downloadBytesPerSecond = 0;
        downloadSkippedFiles = 0;
        downloadResumedFiles = 0;
        downloadStartedAtMs = 0;
        downloadFinishedAtMs = 0;
        receiveEndpoint = "";
        remoteCurrentPath = "";
        remoteCurrentPathMissing = false;
        remoteFiles.clear();
        selectedRemotePaths.clear();
        remoteFileCount = 0;
        remoteDirCount = 0;
        remoteTotalBytes = 0;
        receiveStatus = "Preparing";
        appendLog("info", "Start receiving requested");
        long runId = ++receiveRunId;
        receiveSession = bridge.startP2PReceive(this, passphrase, receiveUseUdp, callback(runId));
        updateKeepScreenOn();
        render();
    }

    private GoncBridge.EventCallback callback(long runId) {
        return new GoncBridge.EventCallback() {
            @Override
            public void onEvent(String level, String message) {
                mainHandler.post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    updateMetricsFromLog(receiveMetrics, message);
                    appendLog(level, message);
                    renderAfterBackgroundUpdate();
                });
            }

            @Override
            public void onP2PReport(String topic, String status, String network, String mode, String peer, long timestamp, long pid) {
                mainHandler.post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    updateMetricsFromReport(receiveMetrics, topic, status, network, mode, peer);
                    renderAfterBackgroundUpdate();
                });
            }

            @Override
            public void onReady(String endpoint) {
                mainHandler.post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    receiveStatus = "Ready";
                    receiveMetrics.p2pStatus = "connected";
                    appendLog("info", "Ready: " + endpoint);
                    loadRemoteFiles(endpoint, runId);
                    render();
                });
            }

            @Override
            public void onStopped() {
                mainHandler.post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    receiveSession = null;
                    receiveStatus = "Idle";
                    stopReceiveWorkers();
                    receiveMetrics.markStopped();
                    updateKeepScreenOn();
                    appendLog("warn", "Session stopped");
                    render();
                });
            }

            @Override
            public void onError(Throwable error) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    receiveSession = null;
                    receiveStatus = "Error";
                    stopReceiveWorkers();
                    remoteListStatus = "Failed";
                    receiveMetrics.p2pStatus = "error";
                    updateKeepScreenOn();
                    appendLog("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    render();
                });
            }
        };
    }

    private void stopSession() {
        GoncBridge.Session current = receiveSession;
        if (current != null) {
            current.stop();
            receiveSession = null;
        }
        if (receiveDownload != null) {
            receiveDownload.stop();
            receiveDownload = null;
        }
        if (remoteListSession != null) {
            remoteListSession.stop();
            remoteListSession = null;
        }
        remoteListStatus = "Stopped";
        downloadStatus = "Stopped";
        downloadBytesPerSecond = 0;
        receiveMetrics.inBps = 0;
        receiveMetrics.outBps = 0;
        receiveMetrics.lastTrafficMs = 0;
        receiveStatus = "Idle";
        receiveMetrics.markStopped();
        updateKeepScreenOn();
        appendLog("warn", "Receive stop requested");
        render();
    }

    private void endAllTasksAndExit() {
        receiveRunId++;

        GoncBridge.Session receive = receiveSession;
        HttpReceiver.Session listSession = remoteListSession;
        HttpReceiver.Session download = receiveDownload;

        receiveSession = null;
        remoteListSession = null;
        receiveDownload = null;

        sendController.endTask();
        if (receive != null) {
            receive.stop();
        }
        vpnServer.endTask();
        if (listSession != null) {
            listSession.stop();
        }
        if (download != null) {
            download.stop();
        }
        if (GoncVpnState.isRunning()) {
            stopVpnService();
        }

        resetTransientStateForFreshLaunch();
        updateKeepScreenOn();
        finish();
    }

    private void resetTransientStateForFreshLaunch() {
        logs.clear();

        sendMode = true;
        vpnMode = false;
        vpnServerMode = false;
        receiveUseUdp = false;
        vpnClient.resetForFreshLaunch();
        sendController.resetForFreshLaunch();
        activityExpanded = false;
        receivePassword = "";
        receivePasswordVisible = false;
        receivePasswordVisibilityToken++;
        vpnServer.resetForFreshLaunch();

        saveTreeUri = null;
        saveLocationLabel = getString(R.string.default_save_location);
        receiveEndpoint = "";
        remoteListStatus = "Idle";
        remoteCurrentPath = "";
        remoteCurrentPathMissing = false;
        remoteFiles.clear();
        selectedRemotePaths.clear();
        remoteFileCount = 0;
        remoteDirCount = 0;
        remoteTotalBytes = 0;

        resumeDownloads = true;
        downloadStatus = "Idle";
        downloadDoneFiles = 0;
        downloadTotalFiles = 0;
        downloadDoneBytes = 0;
        downloadTotalBytes = 0;
        downloadNetworkBytes = 0;
        downloadBytesPerSecond = 0;
        downloadSkippedFiles = 0;
        downloadResumedFiles = 0;
        downloadStartedAtMs = 0;
        downloadFinishedAtMs = 0;

        receiveStatus = "Idle";
        receiveMetrics.reset();
    }

    private void loadRemoteFiles(String endpoint, long runId) {
        loadRemoteFiles(endpoint, runId, "", true, true);
    }

    private void refreshRemoteFiles(String subPath) {
        if (receiveEndpoint == null || !receiveEndpoint.startsWith("http://")) {
            remoteListStatus = "Waiting for peer";
            Toast.makeText(this, R.string.toast_peer_not_connected, Toast.LENGTH_SHORT).show();
            render();
            return;
        }
        if (!"connected".equals(receiveConnectionState())) {
            remoteListStatus = "Waiting for peer";
            Toast.makeText(this, R.string.toast_peer_not_connected, Toast.LENGTH_SHORT).show();
            render();
            return;
        }
        loadRemoteFiles(receiveEndpoint, receiveRunId, subPath, false, false);
    }

    private void loadRemoteFiles(String endpoint, long runId, String subPath, boolean clearSelection, boolean replaceAll) {
        if (endpoint == null || !endpoint.startsWith("http://")) {
            return;
        }
        if (remoteListSession != null) {
            remoteListSession.stop();
        }
        String targetPath = normalizeRemotePath(subPath);
        receiveEndpoint = endpoint;
        remoteListStatus = "Fetching " + displayRemotePath(targetPath);
        remoteCurrentPath = targetPath;
        remoteCurrentPathMissing = false;
        if (replaceAll) {
            remoteFiles.clear();
            remoteFileCount = 0;
            remoteDirCount = 0;
            remoteTotalBytes = 0;
        }
        if (clearSelection) {
            selectedRemotePaths.clear();
        }
        remoteListSession = HttpReceiver.startList(endpoint, targetPath, new HttpReceiver.ListCallback() {
            @Override
            public void onList(List<HttpReceiver.RemoteFile> files, int fileCount, int dirCount, long totalBytes, boolean missing) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    remoteListSession = null;
                    remoteCurrentPathMissing = missing && !targetPath.isEmpty();
                    if (replaceAll) {
                        remoteFiles.clear();
                        remoteFiles.addAll(files);
                    } else {
                        List<String> refreshedPaths = new ArrayList<>();
                        refreshedPaths.add(targetPath);
                        mergeRemoteFiles(refreshedPaths, files);
                    }
                    recalculateRemoteFileSummary();
                    remoteListStatus = remoteCurrentPathMissing ? "Remote path missing" : (files.isEmpty() ? "No remote files" : (replaceAll ? "Remote list ready" : "Remote list refreshed"));
                    updateKeepScreenOn();
                    if (remoteCurrentPathMissing) {
                        appendLog("warn", "Remote path missing " + displayRemotePath(targetPath));
                    } else {
                        appendLog("info", "Remote list ready " + displayRemotePath(targetPath) + ": " + fileCount + " file(s), " + dirCount + " folder(s), " + formatBytes(totalBytes));
                    }
                    render();
                });
            }

            @Override
            public void onError(Throwable error) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    remoteListSession = null;
                    remoteListStatus = "Remote list failed";
                    updateKeepScreenOn();
                    appendLog("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    Toast.makeText(MainActivity.this, R.string.toast_remote_refresh_failed, Toast.LENGTH_SHORT).show();
                    render();
                });
            }
        });
        updateKeepScreenOn();
        render();
    }

    private void mergeRemoteFiles(List<String> refreshedPaths, List<HttpReceiver.RemoteFile> freshFiles) {
        Map<String, HttpReceiver.RemoteFile> merged = new LinkedHashMap<>();
        for (HttpReceiver.RemoteFile file : remoteFiles) {
            String path = normalizeRemotePath(file.path);
            if (!isUnderAnyRefreshPath(path, refreshedPaths)) {
                merged.put(path, file);
            }
        }
        for (HttpReceiver.RemoteFile file : freshFiles) {
            merged.put(normalizeRemotePath(file.path), file);
        }
        remoteFiles.clear();
        remoteFiles.addAll(merged.values());
    }

    private boolean isUnderAnyRefreshPath(String path, List<String> refreshedPaths) {
        String normalized = normalizeRemotePath(path);
        if (refreshedPaths == null || refreshedPaths.isEmpty()) {
            return true;
        }
        for (String refreshPath : refreshedPaths) {
            String target = normalizeRemotePath(refreshPath);
            if (target.isEmpty() || normalized.equals(target) || normalized.startsWith(target + "/")) {
                return true;
            }
        }
        return false;
    }

    private void recalculateRemoteFileSummary() {
        int fileCount = 0;
        int dirCount = 0;
        long totalBytes = 0;
        for (HttpReceiver.RemoteFile file : remoteFiles) {
            if (file.isDir) {
                dirCount++;
            } else {
                fileCount++;
                totalBytes += Math.max(0, file.size);
            }
        }
        remoteFileCount = fileCount;
        remoteDirCount = dirCount;
        remoteTotalBytes = totalBytes;
    }

    private void refreshAndStartDownload(List<String> paths) {
        if (receiveEndpoint == null || !receiveEndpoint.startsWith("http://")) {
            remoteListStatus = "Waiting for peer";
            Toast.makeText(this, R.string.toast_peer_not_connected, Toast.LENGTH_SHORT).show();
            render();
            return;
        }
        if (!"connected".equals(receiveConnectionState())) {
            remoteListStatus = "Waiting for peer";
            Toast.makeText(this, R.string.toast_peer_not_connected, Toast.LENGTH_SHORT).show();
            render();
            return;
        }
        if (paths == null || paths.isEmpty()) {
            Toast.makeText(this, R.string.toast_select_download_files, Toast.LENGTH_SHORT).show();
            return;
        }
        if (!ensureDefaultSavePermission()) {
            return;
        }
        if (currentDirectoryFiles().isEmpty() && paths.size() == 1 && normalizeRemotePath(paths.get(0)).equals(normalizeRemotePath(remoteCurrentPath))) {
            Toast.makeText(this, R.string.toast_no_remote_files, Toast.LENGTH_SHORT).show();
            return;
        }
        if (remoteListSession != null) {
            remoteListSession.stop();
        }
        List<String> normalizedPaths = new ArrayList<>();
        for (String path : paths) {
            normalizedPaths.add(normalizeRemotePath(path));
        }
        remoteListStatus = "Refreshing remote file list";
        downloadStatus = "Preparing download";
        remoteListSession = HttpReceiver.startList(receiveEndpoint, normalizedPaths, new HttpReceiver.ListCallback() {
            @Override
            public void onList(List<HttpReceiver.RemoteFile> files, int fileCount, int dirCount, long totalBytes, boolean missing) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(receiveRunId)) {
                        return;
                    }
                    remoteListSession = null;
                    mergeRemoteFiles(normalizedPaths, files);
                    recalculateRemoteFileSummary();
                    remoteCurrentPathMissing = missing && normalizedPaths.size() == 1 && normalizeRemotePath(remoteCurrentPath).equals(normalizedPaths.get(0)) && !remoteCurrentPath.isEmpty();
                    remoteListStatus = remoteCurrentPathMissing ? "Remote path missing" : (files.isEmpty() ? "No remote files" : "Remote list refreshed");
                    updateKeepScreenOn();
                    if (files.isEmpty()) {
                        downloadStatus = "Idle";
                        Toast.makeText(MainActivity.this, R.string.toast_no_remote_files, Toast.LENGTH_SHORT).show();
                        render();
                        return;
                    }
                    appendLog("info", "Remote list refreshed before download: " + fileCount + " file(s), " + dirCount + " folder(s), " + formatBytes(totalBytes));
                    startReceiveDownload(receiveEndpoint, receiveRunId, files);
                    render();
                });
            }

            @Override
            public void onError(Throwable error) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(receiveRunId)) {
                        return;
                    }
                    remoteListSession = null;
                    remoteListStatus = "Remote refresh failed";
                    downloadStatus = "Idle";
                    updateKeepScreenOn();
                    appendLog("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    Toast.makeText(MainActivity.this, R.string.toast_remote_refresh_failed, Toast.LENGTH_SHORT).show();
                    render();
                });
            }
        });
        updateKeepScreenOn();
        render();
    }

    private void startReceiveDownload(String endpoint, long runId, List<HttpReceiver.RemoteFile> files) {
        if (endpoint == null || !endpoint.startsWith("http://")) {
            return;
        }
        if (files == null || files.isEmpty()) {
            Toast.makeText(this, R.string.toast_select_download_files, Toast.LENGTH_SHORT).show();
            return;
        }
        if (!ensureDefaultSavePermission()) {
            return;
        }
        if (receiveDownload != null) {
            receiveDownload.stop();
        }
        receiveEndpoint = endpoint;
        downloadStatus = "Preparing download";
        downloadDoneFiles = 0;
        downloadTotalFiles = 0;
        downloadDoneBytes = 0;
        downloadTotalBytes = 0;
        downloadNetworkBytes = 0;
        downloadBytesPerSecond = 0;
        downloadSkippedFiles = 0;
        downloadResumedFiles = 0;
        downloadStartedAtMs = System.currentTimeMillis();
        downloadFinishedAtMs = 0;
        receiveDownload = HttpReceiver.start(this, endpoint, saveTreeUri, files, resumeDownloads, new HttpReceiver.Callback() {
            @Override
            public void onProgress(int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current) {
                queueDownloadProgress(runId, doneFiles, totalFiles, doneBytes, totalBytes, networkBytes, bytesPerSecond, current);
            }

            @Override
            public void onComplete(int totalFiles, long totalBytes, long networkBytes, int skippedFiles, int resumedFiles) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    if (receiveDownload == null && "Stopped".equals(downloadStatus)) {
                        return;
                    }
                    receiveDownload = null;
                    downloadDoneFiles = totalFiles;
                    downloadTotalFiles = totalFiles;
                    downloadDoneBytes = totalBytes;
                    downloadTotalBytes = totalBytes;
                    downloadNetworkBytes = networkBytes;
                    downloadBytesPerSecond = 0;
                    downloadFinishedAtMs = System.currentTimeMillis();
                    downloadSkippedFiles = skippedFiles;
                    downloadResumedFiles = resumedFiles;
                    receiveMetrics.inBps = 0;
                    receiveMetrics.outBps = 0;
                    receiveMetrics.lastTrafficMs = 0;
                    downloadStatus = "Receive complete";
                    updateKeepScreenOn();
                    appendLog("info", "Receive complete: " + totalFiles + " file(s), " + HttpReceiver.formatBytes(totalBytes) + ", skipped " + skippedFiles + ", resumed " + resumedFiles);
                    render();
                });
            }

            @Override
            public void onError(Throwable error) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    if (receiveDownload == null && "Stopped".equals(downloadStatus)) {
                        return;
                    }
                    receiveDownload = null;
                    downloadStatus = "Receive failed";
                    downloadBytesPerSecond = 0;
                    receiveMetrics.inBps = 0;
                    receiveMetrics.outBps = 0;
                    receiveMetrics.lastTrafficMs = 0;
                    updateKeepScreenOn();
                    appendLog("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    render();
                });
            }
        });
        updateKeepScreenOn();
    }

    private void queueDownloadProgress(long runId, int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current) {
        synchronized (downloadProgressLock) {
            pendingDownloadProgress = new DownloadProgressSnapshot(runId, doneFiles, totalFiles, doneBytes, totalBytes, networkBytes, bytesPerSecond, current);
            if (downloadProgressApplyPending) {
                return;
            }
            downloadProgressApplyPending = true;
        }
        mainHandler.post(this::applyLatestDownloadProgress);
    }

    private void applyLatestDownloadProgress() {
        DownloadProgressSnapshot progress;
        synchronized (downloadProgressLock) {
            progress = pendingDownloadProgress;
            pendingDownloadProgress = null;
            downloadProgressApplyPending = false;
        }
        if (progress == null || !isCurrentRun(progress.runId) || receiveDownload == null) {
            return;
        }
        downloadDoneFiles = progress.doneFiles;
        downloadTotalFiles = progress.totalFiles;
        downloadDoneBytes = progress.doneBytes;
        downloadTotalBytes = progress.totalBytes;
        downloadNetworkBytes = progress.networkBytes;
        downloadBytesPerSecond = progress.bytesPerSecond;
        receiveMetrics.inBps = progress.bytesPerSecond;
        receiveMetrics.lastTrafficMs = System.currentTimeMillis();
        downloadStatus = progress.current == null || progress.current.trim().isEmpty() ? "Preparing download" : "Receiving " + progress.current;
        renderDownloadProgress();
    }

    private boolean ensureDefaultSavePermission() {
        if (saveTreeUri != null || Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            return true;
        }
        if (checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED) {
            return true;
        }
        requestPermissions(new String[]{Manifest.permission.WRITE_EXTERNAL_STORAGE}, REQUEST_STORAGE_PERMISSION);
        return false;
    }

    private void stopReceiveDownload() {
        if (receiveDownload != null) {
            receiveDownload.stop();
            receiveDownload = null;
        }
        synchronized (downloadProgressLock) {
            pendingDownloadProgress = null;
            downloadProgressApplyPending = false;
        }
        downloadStatus = "Stopped";
        downloadFinishedAtMs = System.currentTimeMillis();
        downloadBytesPerSecond = 0;
        receiveMetrics.inBps = 0;
        receiveMetrics.outBps = 0;
        receiveMetrics.lastTrafficMs = 0;
        updateKeepScreenOn();
        appendLog("warn", "Receive download stop requested");
        render();
    }

    private void stopReceiveWorkers() {
        if (remoteListSession != null) {
            remoteListSession.stop();
            remoteListSession = null;
        }
        if (receiveDownload != null) {
            receiveDownload.stop();
            receiveDownload = null;
        }
        downloadBytesPerSecond = 0;
        receiveMetrics.inBps = 0;
        receiveMetrics.outBps = 0;
        receiveMetrics.lastTrafficMs = 0;
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
        appendLog("info", "VPN start requested: " + profile.displayName(this));
        GoncVpnState.startConnecting(profile.displayName(this));
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(intent);
        } else {
            startService(intent);
        }
        updateKeepScreenOn();
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
        updateKeepScreenOn();
        render();
    }

    private void pastePassword() {
        if (receiveSession != null) {
            return;
        }
        ClipboardManager clipboard = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (clipboard == null || !clipboard.hasPrimaryClip() || clipboard.getPrimaryClip() == null) {
            Toast.makeText(this, R.string.toast_clipboard_empty, Toast.LENGTH_SHORT).show();
            return;
        }
        ClipData clip = clipboard.getPrimaryClip();
        if (clip.getItemCount() == 0) {
            Toast.makeText(this, R.string.toast_clipboard_empty, Toast.LENGTH_SHORT).show();
            return;
        }
        CharSequence value = clip.getItemAt(0).coerceToText(this);
        if (value != null) {
            receivePassword = value.toString().trim();
            revealPasswordTemporarily();
            appendLog("info", "Passphrase pasted");
            render();
        }
    }

    private void showPasswordQr() {
        showPassphraseQr(receivePassword.trim());
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

    private void scanPassword() {
        if (receiveSession != null) {
            return;
        }
        scanPassphrase(result -> {
            if (receiveSession != null) {
                return;
            }
            receivePassword = result.trim();
            revealPasswordTemporarily();
            appendLog("info", "Passphrase scanned");
            render();
        });
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
            return GoncVpnState.status();
        }
        if (vpnServerMode) {
            return vpnServer.status();
        }
        return sendMode ? sendController.status() : receiveStatus;
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

    private boolean isCurrentRun(long runId) {
        return receiveRunId == runId;
    }

    private boolean isActiveRun(long runId) {
        return receiveRunId == runId && receiveSession != null;
    }

    private boolean isAnySessionRunning() {
        return sendController.isRunning() || receiveSession != null || vpnServer.isRunning() || remoteListSession != null || receiveDownload != null || GoncVpnState.isRunning();
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
        return receiveMetrics;
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
        if (!sendMode && receiveDownload == null) {
            return 0;
        }
        if (!sendMode && downloadBytesPerSecond > 0) {
            return downloadBytesPerSecond;
        }
        if (System.currentTimeMillis() - metrics.lastTrafficMs > 4000) {
            return 0;
        }
        return sendMode ? metrics.outBps : Math.max(metrics.inBps, metrics.outBps);
    }

    private String displayStatus(TransferMetrics metrics) {
        String status = normalizeMetricStatus(metrics.p2pStatus);
        if ("negotiating".equals(status)
                || "connected".equals(status)
                || "failed".equals(status)
                || "error".equals(status)
                || "disconnected".equals(status)) {
            return status;
        }
        if (metrics.connectingCount > 0 && metrics.connectedCount == 0) {
            return "connecting";
        }
        return metrics.p2pStatus;
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
        renderAfterBackgroundUpdate();
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
    public void updateKeepScreenOn() {
        if (isAnySessionRunning()) {
            getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        } else {
            getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        }
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

    private void updateDownloadProgressViews() {
        long total = Math.max(downloadTotalBytes, 0);
        long done = Math.max(downloadDoneBytes, 0);
        if (downloadStatusView != null) {
            downloadStatusView.setText(displayDownloadStatus(downloadStatus));
        }
        if (downloadDetailView != null) {
            downloadDetailView.setText(downloadProgressDetail(done, total));
        }
        if (downloadProgressBar != null) {
            downloadProgressBar.setProgress(progressValue(done, total));
        }
        if (downloadSummaryView != null) {
            downloadSummaryView.setText(downloadProgressSummary(done, total));
        }
    }

    private String downloadProgressDetail(long done, long total) {
        String detailText = getString(R.string.progress_detail, formatPercent(done, total), downloadDoneFiles, downloadTotalFiles, formatBytes(done), formatBytes(total));
        if (downloadSkippedFiles > 0 || downloadResumedFiles > 0) {
            detailText = getString(R.string.progress_detail_extra, detailText, downloadSkippedFiles, downloadResumedFiles);
        }
        return detailText;
    }

    private String downloadProgressSummary(long done, long total) {
        if ("Receive complete".equals(downloadStatus)) {
            return getString(R.string.progress_completed_summary,
                    formatDuration(downloadElapsedSeconds()),
                    formatRate(downloadAverageBytesPerSecond()));
        }
        if ("Stopped".equals(downloadStatus)) {
            return getString(R.string.progress_stopped_summary, formatDuration(downloadElapsedSeconds()));
        }
        double speedValue = currentDownloadSpeed();
        String speedText = getString(R.string.progress_speed, formatRate(speedValue));
        String remaining = formatRemaining(done, total, speedValue);
        if (!remaining.isEmpty()) {
            speedText = speedText + "  |  " + remaining;
        }
        return speedText;
    }

    private long downloadElapsedSeconds() {
        if (downloadStartedAtMs <= 0) {
            return 0;
        }
        long end = downloadFinishedAtMs > 0 ? downloadFinishedAtMs : System.currentTimeMillis();
        return Math.max(0, (end - downloadStartedAtMs + 999) / 1000);
    }

    private double downloadAverageBytesPerSecond() {
        long elapsedMs;
        if (downloadStartedAtMs <= 0 || downloadFinishedAtMs <= downloadStartedAtMs) {
            elapsedMs = Math.max(1, downloadElapsedSeconds() * 1000);
        } else {
            elapsedMs = downloadFinishedAtMs - downloadStartedAtMs;
        }
        return Math.max(0, downloadNetworkBytes * 1000.0 / elapsedMs);
    }

    private int progressValue(long done, long total) {
        return ui.progressValue(done, total);
    }

    private String formatRemaining(long done, long total, double bytesPerSecond) {
        if (total <= 0 || done >= total || receiveDownload == null) {
            return "";
        }
        if (bytesPerSecond <= 1) {
            return getString(R.string.progress_remaining_estimating);
        }
        long seconds = Math.max(1, (long) Math.ceil((total - done) / bytesPerSecond));
        return getString(R.string.progress_remaining, formatDuration(seconds));
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

    private static final class SelectionSummary {
        int files;
        long bytes;
    }

    private static final class DownloadProgressSnapshot {
        final long runId;
        final int doneFiles;
        final int totalFiles;
        final long doneBytes;
        final long totalBytes;
        final long networkBytes;
        final double bytesPerSecond;
        final String current;

        DownloadProgressSnapshot(long runId, int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current) {
            this.runId = runId;
            this.doneFiles = doneFiles;
            this.totalFiles = totalFiles;
            this.doneBytes = doneBytes;
            this.totalBytes = totalBytes;
            this.networkBytes = networkBytes;
            this.bytesPerSecond = bytesPerSecond;
            this.current = current;
        }
    }
}
