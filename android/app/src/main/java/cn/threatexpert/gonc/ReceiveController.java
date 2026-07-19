package cn.threatexpert.gonc;

import android.content.ActivityNotFoundException;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.Uri;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.PopupMenu;
import android.widget.ScrollView;
import android.widget.TextView;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.function.BiConsumer;

/**
 * Receive ("receive files") module: the largest and most intricate screen. Owns
 * the P2P receive session, the remote file-browser state, and the multi-worker
 * download pipeline. The host keeps the save-location picker (Activity-coupled
 * SAF), feeds the chosen folder in via {@link #setSaveLocation}, and renders the
 * shared activity/metrics chrome by pulling {@link #metrics()}/{@link #endpoint()}.
 */
final class ReceiveController {
    private ModuleHost host;
    private final TransferMetrics receiveMetrics = new TransferMetrics();
    private final Object downloadProgressLock = new Object();

    private boolean receiveUseUdp;
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
    private final Map<String, HttpReceiver.ReceivedTarget> receivedTargets = new LinkedHashMap<>();
    private long receivedTargetCheckId;
    private boolean receivedCompletionRefreshPending;
    private long receivedCompletionRefreshToken;
    private boolean shutdown;
    private final Set<String> selectedRemotePaths = new HashSet<>();
    private final Set<String> excludedRemotePaths = new HashSet<>();
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
    private int downloadFailedFiles;
    private long downloadStartedAtMs;
    private long downloadFinishedAtMs;
    private String receiveStatus = "Idle";
    private GoncBridge.Session receiveSession;
    private HttpReceiver.Session remoteListSession;
    private HttpReceiver.Session receiveDownload;
    private long receiveDownloadId;
    private long receiveRunId;
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
    private TextView remoteFilesSummaryTextView;
    private TextView remoteFilesTotalSummaryTextView;
    private Button downloadSelectedButton;
    private final Map<String, CheckBox> remoteFileCheckboxes = new LinkedHashMap<>();
    private boolean updatingRemoteSelectionViews;

    ReceiveController(ModuleHost host) {
        this.host = host;
        saveLocationLabel = string(R.string.default_save_location);
    }

    /** Rebind to the current host after an Activity recreation (config change). */
    void attach(ModuleHost host) {
        this.host = host;
    }

    // --- exposed state for the host's shared chrome -----------------------

    TransferMetrics metrics() {
        return receiveMetrics;
    }

    String status() {
        return receiveStatus;
    }

    String endpoint() {
        return receiveEndpoint;
    }

    boolean isRunning() {
        return receiveSession != null;
    }

    /** True when any receive worker (session, remote listing, or download) is live. */
    boolean isBusy() {
        return receiveSession != null || remoteListSession != null || receiveDownload != null
                || receivedCompletionRefreshPending;
    }

    boolean isDownloading() {
        return receiveDownload != null || receivedCompletionRefreshPending;
    }

    /**
     * Foreground-service contribution: null when no receive worker is live. The
     * dot mirrors the in-app connection chip (red/green/yellow); progress is a
     * 0..100 bar while downloading with a known total, otherwise -1 (so the
     * notification just shows the connection status).
     */
    GoncForegroundService.State foregroundState() {
        if (!isBusy()) {
            return null;
        }
        GoncForegroundService.Dot dot =
                GoncForegroundService.dotForState(host.ui().connectionState(receiveMetrics));
        int progress = -1;
        if (receiveDownload != null && downloadTotalBytes > 0) {
            long pct = downloadDoneBytes * 100 / downloadTotalBytes;
            progress = (int) Math.max(0, Math.min(100, pct));
        }
        return new GoncForegroundService.State(dot, progress);
    }

    double currentDownloadSpeed() {
        return receiveDownload == null ? 0 : downloadBytesPerSecond;
    }

    /** Whether the secondary remote-files card should be shown next to the connect card. */
    boolean shouldShowRemoteFilesPanel() {
        return !receiveEndpoint.trim().isEmpty() || !remoteFiles.isEmpty() || !"Idle".equals(remoteListStatus);
    }

    void setSaveLocation(Uri uri, String label) {
        if (!ReceivedFileActionState.canStartNewConnection(
                receiveDownload != null, receivedCompletionRefreshPending)) {
            return;
        }
        saveTreeUri = uri;
        saveLocationLabel = label;
    }

    // --- lifecycle --------------------------------------------------------

    /** Stop every receive worker quietly (no UI/log), e.g. on Activity destroy. */
    void shutdown() {
        shutdown = true;
        receivedTargetCheckId++;
        GoncBridge.Session receive = receiveSession;
        HttpReceiver.Session list = remoteListSession;
        HttpReceiver.Session download = receiveDownload;
        receiveSession = null;
        remoteListSession = null;
        receiveDownload = null;
        receiveDownloadId++;
        if (receive != null) {
            receive.stop();
        }
        if (list != null) {
            list.stop();
        }
        if (download != null) {
            download.stop();
        }
    }

    /** Invalidate the running run and stop everything, e.g. when ending all tasks. */
    void endTask() {
        receiveRunId++;
        shutdown();
    }

    /** Reset transient state for a fresh launch. */
    void resetForFreshLaunch() {
        receiveUseUdp = false;
        receivePassword = "";
        receivePasswordVisible = false;
        receivePasswordVisibilityToken++;
        saveTreeUri = null;
        saveLocationLabel = string(R.string.default_save_location);
        receiveEndpoint = "";
        remoteListStatus = "Idle";
        remoteCurrentPath = "";
        remoteCurrentPathMissing = false;
        remoteFiles.clear();
        selectedRemotePaths.clear();
        excludedRemotePaths.clear();
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
        downloadFailedFiles = 0;
        downloadStartedAtMs = 0;
        downloadFinishedAtMs = 0;
        receiveStatus = "Idle";
        receiveMetrics.reset();
    }

    // --- view -------------------------------------------------------------

    View panel() {
        LinearLayout box = column();
        box.addView(receivePanel());
        if (shouldShowRemoteFilesPanel()) {
            box.addView(remoteFilesPanel());
        }
        return box;
    }

    /** In-place refresh of this module's cached views during background updates. */
    void updateDynamicViews() {
        if (receiveConnectionDotView != null) {
            receiveConnectionDotView.setBackground(rounded(receiveConnectionColor(), dp(6), 0, 0));
        }
        if (receiveConnectionLabelView != null) {
            receiveConnectionLabelView.setText(receiveConnectionLabel());
        }
    }

    private View receivePanel() {
        LinearLayout card = card();
        if (receiveSession != null) {
            card.addView(receiveSessionBarView());
            return card;
        }
        card.addView(sectionBoundaryTitle(string(R.string.save_location_config), false), blockParams(0));
        LinearLayout save = row();
        TextView saveText = text(string(R.string.save_to, saveLocationLabel), 14, Color.rgb(38, 56, 79), Typeface.BOLD);
        saveText.setSingleLine(true);
        save.addView(saveText, new LinearLayout.LayoutParams(0, dp(42), 1));
        Button choose = secondaryButton(string(R.string.choose));
        setControlEnabled(choose, canStartReceiveConnection());
        choose.setOnClickListener(v -> host.pickSaveLocation());
        LinearLayout.LayoutParams chooseParams = new LinearLayout.LayoutParams(dp(104), dp(42));
        chooseParams.setMargins(dp(8), 0, 0, 0);
        save.addView(choose, chooseParams);
        card.addView(save, blockParams());
        card.addView(sectionBoundaryTitle(string(R.string.passphrase_config), true), blockParams(dp(14)));
        card.addView(passwordField());
        card.addView(sectionDivider(), dividerParams(dp(12)));
        card.addView(protocolToggle());

        Button primary = receiveSession == null ? primaryButton(string(R.string.connect)) : dangerButton(string(R.string.disconnect));
        setControlEnabled(primary, canStartReceiveConnection());
        primary.setOnClickListener(v -> {
            if (canStartReceiveConnection()) {
                startP2PReceive();
            } else {
                if (receiveSession != null) {
                    stopSession();
                }
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

        View dot = new View(context());
        dot.setBackground(rounded(receiveConnectionColor(), dp(6), 0, 0));
        receiveConnectionDotView = dot;
        row.addView(dot, new LinearLayout.LayoutParams(dp(12), dp(12)));

        TextView label = text(receiveConnectionLabel(), 14, ink(), Typeface.BOLD);
        label.setSingleLine(false);
        receiveConnectionLabelView = label;
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        labelParams.setMargins(dp(10), 0, dp(8), 0);
        row.addView(label, labelParams);

        Button passphrase = secondaryButton(string(R.string.passphrase));
        passphrase.setOnClickListener(v -> showPasswordQr());
        row.addView(passphrase, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));

        Button disconnect = dangerButton(string(R.string.disconnect));
        disconnect.setTextSize(14);
        disconnect.setOnClickListener(v -> stopSession());
        LinearLayout.LayoutParams disconnectParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38));
        disconnectParams.setMargins(dp(8), 0, 0, 0);
        row.addView(disconnect, disconnectParams);
        return row;
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

        ProgressBar progress = new ProgressBar(context(), null, android.R.attr.progressBarStyleHorizontal);
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
        card.addView(sectionTitle(string(R.string.remote_files)));

        LinearLayout pathRow = row();
        pathRow.setGravity(Gravity.CENTER_VERTICAL);
        pathRow.addView(remoteBreadcrumbView(), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button selectAll = quietTouchButton(string(R.string.select_all));
        setControlEnabled(selectAll, canClickRemoteAction() && !visibleRemoteFiles().isEmpty());
        selectAll.setOnClickListener(v -> selectVisibleRemoteFiles());
        pathRow.addView(selectAll, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(44)));
        Button invert = quietTouchButton(string(R.string.invert_selection));
        setControlEnabled(invert, canClickRemoteAction() && !visibleRemoteFiles().isEmpty());
        invert.setOnClickListener(v -> invertVisibleRemoteFiles());
        pathRow.addView(invert, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(44)));
        Button refresh = quietTouchButton(string(R.string.refresh));
        setControlEnabled(refresh, canClickRemoteAction());
        refresh.setOnClickListener(v -> refreshRemoteFiles(remoteCurrentPath));
        pathRow.addView(refresh, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(44)));
        card.addView(pathRow, blockParams(dp(8)));
        if (shouldShowRemoteListStatus()) {
            card.addView(remoteListStatusView(), blockParams(dp(4)));
        }

        boolean currentRoot = normalizeRemotePath(remoteCurrentPath).isEmpty();
        if (remoteFiles.isEmpty() && currentRoot && !remoteCurrentPathMissing) {
            card.addView(text(string(R.string.remote_list_waiting), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
            return card;
        }

        List<HttpReceiver.RemoteFile> visible = visibleRemoteFiles();
        LinearLayout list = column();
        remoteFileCheckboxes.clear();
        if (!remoteCurrentPath.isEmpty()) {
            list.addView(parentDirectoryRow());
        }
        if (remoteCurrentPathMissing) {
            list.addView(text(string(R.string.folder_missing), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
        } else if (visible.isEmpty()) {
            list.addView(text(currentRoot ? string(R.string.toast_no_remote_files) : string(R.string.folder_empty), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
        } else {
            for (HttpReceiver.RemoteFile file : visible) {
                list.addView(remoteFileRow(file));
            }
        }
        ScrollView listScroll = new InterceptingScrollView(context());
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
            Button stop = warningButton(string(R.string.stop_download));
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
            Button selected = outlineButton(string(R.string.download_selected));
            downloadSelectedButton = selected;
            boolean canDownloadSelected = canStartRemoteDownload() && !selectedRemotePaths.isEmpty();
            setControlEnabled(selected, canDownloadSelected);
            selected.setOnClickListener(v -> startSelectedDownload());

            Button all = primaryButton(receiveAllButtonLabel());
            setControlEnabled(all, canStartRemoteDownload());
            all.setOnClickListener(v -> refreshAndStartDownload(currentDownloadPaths(), false));

            actions.addView(selected, new LinearLayout.LayoutParams(0, dp(42), 1));
            actions.addView(all, actionParams(dp(42)));
        }
        box.addView(actions, blockParams(dp(10)));
        return box;
    }

    private String receiveAllButtonLabel() {
        return normalizeRemotePath(remoteCurrentPath).isEmpty()
                ? string(R.string.receive_all)
                : string(R.string.receive_current_dir);
    }

    private View parentDirectoryRow() {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(12), dp(10), dp(12), dp(10));
        row.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));

        TextView icon = text("↰", 20, Color.rgb(32, 101, 165), Typeface.BOLD);
        icon.setGravity(Gravity.CENTER);
        row.addView(icon, new LinearLayout.LayoutParams(dp(34), dp(42)));

        LinearLayout labels = column();
        TextView name = text(string(R.string.parent_directory), 13, Color.rgb(32, 101, 165), Typeface.BOLD);
        labels.addView(name);
        labels.addView(text(parentPath(remoteCurrentPath).isEmpty() ? "/" : displayRemotePath(parentPath(remoteCurrentPath)), 12, muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row.addView(text("›", 22, muted(), Typeface.BOLD), new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT));
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
        HttpReceiver.ReceivedTarget target = receivedTargets.get(normalizedPath);
        boolean receivedMarkerVisible = ReceivedFileActionState.markerVisible(target != null);
        boolean receivedActionsEnabled = ReceivedFileActionState.actionsEnabled(
                target != null, receiveDownload != null, receivedCompletionRefreshPending);
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(10), dp(6), dp(10), dp(6));
        row.setBackground(rounded(file.isDir ? Color.rgb(246, 250, 255) : Color.WHITE, dp(7), Color.rgb(226, 232, 240), 1));

        CheckBox checkBox = new CheckBox(context());
        checkBox.setChecked(isPathSelectedForDownload(normalizedPath));
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
                excludedRemotePaths.remove(normalizedPath);
                selectedRemotePaths.add(normalizedPath);
            } else {
                selectedRemotePaths.remove(normalizedPath);
                if (hasSelectedParent(normalizedPath)) {
                    excludedRemotePaths.add(normalizedPath);
                } else {
                    excludedRemotePaths.remove(normalizedPath);
                }
            }
            updateRemoteSelectionViews();
        });
        row.addView(checkBox, new LinearLayout.LayoutParams(dp(42), dp(42)));

        TextView icon = text(file.isDir ? "📁" : "📄", 20, file.isDir ? Color.rgb(32, 101, 165) : muted(), Typeface.NORMAL);
        icon.setGravity(Gravity.CENTER);
        row.addView(icon, new LinearLayout.LayoutParams(dp(34), dp(42)));

        LinearLayout labels = column();
        TextView name = text(baseName(normalizedPath), 13, file.isDir ? Color.rgb(32, 101, 165) : ink(), Typeface.BOLD);
        name.setSingleLine(true);
        labels.addView(name);
        String detail = file.isDir ? string(R.string.folder) : formatBytes(file.size);
        if (!file.isDir && file.modifiedMs > 0) {
            detail = detail + "  ·  " + formatTimestamp(file.modifiedMs);
        }
        labels.addView(text(detail, 12, muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        if (!file.isDir && receivedMarkerVisible) {
            View.OnClickListener open = v -> {
                if (canUseReceivedFileActions()) {
                    openReceivedFile(file, false);
                }
            };
            icon.setOnClickListener(open);
            icon.setContentDescription(string(R.string.open) + " " + baseName(normalizedPath));
            icon.setClickable(true);
            icon.setFocusable(true);
            setControlEnabled(icon, receivedActionsEnabled);
            labels.setOnClickListener(open);
            labels.setContentDescription(string(R.string.open) + " " + baseName(normalizedPath));
            labels.setClickable(true);
            labels.setFocusable(true);
            setControlEnabled(labels, receivedActionsEnabled);
            TextView available = text("\u2713", 14, Color.rgb(32, 151, 102), Typeface.BOLD);
            available.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
            row.addView(available);
            Button more = quietTouchButton("\u22ee");
            more.setContentDescription(string(R.string.received_file_actions));
            more.setOnClickListener(v -> {
                if (canUseReceivedFileActions()) {
                    showReceivedFileMenu(more, file);
                }
            });
            setControlEnabled(more, receivedActionsEnabled);
            row.addView(more, new LinearLayout.LayoutParams(dp(42), dp(42)));
        }
        if (file.isDir) {
            row.addView(text("›", 22, Color.rgb(32, 101, 165), Typeface.BOLD), new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT));
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

    private void showReceivedFileMenu(View anchor, HttpReceiver.RemoteFile file) {
        if (!canUseReceivedFileActions()) {
            return;
        }
        PopupMenu menu = new PopupMenu(context(), anchor);
        menu.getMenu().add(0, 1, 0, R.string.open);
        menu.getMenu().add(0, 2, 1, R.string.open_with);
        menu.getMenu().add(0, 3, 2, R.string.share_file);
        menu.getMenu().add(0, 4, 3, R.string.file_information);
        menu.setOnMenuItemClickListener(item -> {
            switch (item.getItemId()) {
                case 1:
                    openReceivedFile(file, false);
                    return true;
                case 2:
                    openReceivedFile(file, true);
                    return true;
                case 3:
                    withCurrentReceivedTarget(
                            file, (target, saveLabel) -> ReceivedFileActions.share(context(), target));
                    return true;
                case 4:
                    withCurrentReceivedTarget(
                            file, (target, saveLabel) ->
                                    ReceivedFileActions.showInfo(context(), target, saveLabel));
                    return true;
                default:
                    return false;
            }
        });
        menu.show();
    }

    private void openReceivedFile(HttpReceiver.RemoteFile file, boolean chooser) {
        withCurrentReceivedTarget(
                file, (target, saveLabel) ->
                        ReceivedFileActions.open(context(), target, chooser));
    }

    private void withCurrentReceivedTarget(
            HttpReceiver.RemoteFile file,
            BiConsumer<HttpReceiver.ReceivedTarget, String> action) {
        if (!canUseReceivedFileActions()) {
            return;
        }
        long runId = receiveRunId;
        long checkId = ++receivedTargetCheckId;
        String checkedPath = normalizeRemotePath(remoteCurrentPath);
        Uri tree = saveTreeUri;
        String checkedTree = treeSnapshot(tree);
        String checkedSaveLabel = saveLocationLabel;
        Context checkContext = applicationContext();
        new Thread(() -> {
            Map<String, HttpReceiver.ReceivedTarget> checked;
            RuntimeException resolutionFailure = null;
            try {
                checked = HttpReceiver.findReceivedTargets(
                        checkContext, tree, Collections.singletonList(file));
            } catch (RuntimeException error) {
                checked = Collections.emptyMap();
                resolutionFailure = error;
            }
            Map<String, HttpReceiver.ReceivedTarget> resolved = checked;
            RuntimeException failure = resolutionFailure;
            host.mainHandler().post(() -> {
                if (!isReceivedTargetCheckCurrent(
                        runId, checkId, checkedPath, checkedTree, checkedSaveLabel)) {
                    return;
                }
                if (!canUseReceivedFileActions()) {
                    return;
                }
                String normalizedPath = normalizeRemotePath(file.path);
                if (failure != null) {
                    removeReceivedTarget(normalizedPath);
                    return;
                }
                HttpReceiver.ReceivedTarget target = resolved.get(normalizedPath);
                if (target == null) {
                    removeReceivedTarget(normalizedPath);
                    return;
                }
                receivedTargets.put(normalizedPath, target);
                try {
                    action.accept(target, checkedSaveLabel);
                } catch (ActivityNotFoundException error) {
                    host.toast(R.string.toast_received_file_no_handler);
                } catch (SecurityException | IllegalArgumentException error) {
                    removeReceivedTarget(normalizedPath);
                }
            });
        }, "gonc-received-file-action-check").start();
    }

    private void removeReceivedTarget(String normalizedPath) {
        receivedTargets.remove(normalizedPath);
        host.toast(R.string.toast_received_file_unavailable);
        host.requestRender();
    }

    private boolean canUseReceivedFileActions() {
        return !shutdown && ReceivedFileActionState.actionsEnabled(
                true, receiveDownload != null, receivedCompletionRefreshPending);
    }

    private void refreshReceivedTargets(long runId, String checkedPath) {
        refreshReceivedTargets(runId, checkedPath, 0L);
    }

    private void refreshReceivedTargets(
            long runId, String checkedPath, long completionRefreshToken) {
        long checkId = ++receivedTargetCheckId;
        List<HttpReceiver.RemoteFile> snapshot = new ArrayList<>(visibleRemoteFiles());
        Uri tree = saveTreeUri;
        String normalizedCheckedPath = normalizeRemotePath(checkedPath);
        String checkedTree = treeSnapshot(tree);
        String checkedSaveLabel = saveLocationLabel;
        Context checkContext = applicationContext();
        new Thread(() -> {
            Map<String, HttpReceiver.ReceivedTarget> checked;
            RuntimeException resolutionFailure = null;
            try {
                checked = HttpReceiver.findReceivedTargets(checkContext, tree, snapshot);
            } catch (RuntimeException error) {
                checked = Collections.emptyMap();
                resolutionFailure = error;
            }
            Map<String, HttpReceiver.ReceivedTarget> resolved = checked;
            RuntimeException failure = resolutionFailure;
            host.mainHandler().post(() -> {
                if (!isReceivedTargetCheckCurrent(
                        runId, checkId, normalizedCheckedPath, checkedTree, checkedSaveLabel)) {
                    return;
                }
                if (completionRefreshToken == 0L
                        && (receiveDownload != null || receivedCompletionRefreshPending)) {
                    return;
                }
                if (failure == null) {
                    for (HttpReceiver.RemoteFile file : snapshot) {
                        receivedTargets.remove(normalizeRemotePath(file.path));
                    }
                    receivedTargets.putAll(resolved);
                }
                if (ReceivedFileActionState.ownsCompletionRefresh(
                        receivedCompletionRefreshToken, completionRefreshToken)) {
                    receivedCompletionRefreshPending = false;
                }
                host.requestRender();
            });
        }, "gonc-received-file-check").start();
    }

    private boolean isReceivedTargetCheckCurrent(
            long runId,
            long checkId,
            String checkedPath,
            String checkedTree,
            String checkedSaveLabel) {
        return ReceivedTargetCheckGuard.isCurrent(
                runId,
                receiveRunId,
                checkId,
                receivedTargetCheckId,
                checkedPath,
                normalizeRemotePath(remoteCurrentPath),
                checkedTree,
                treeSnapshot(saveTreeUri),
                checkedSaveLabel,
                saveLocationLabel,
                shutdown);
    }

    private String treeSnapshot(Uri tree) {
        return tree == null ? null : tree.toString();
    }

    private Context applicationContext() {
        Context current = context();
        Context application = current.getApplicationContext();
        return application == null ? current : application;
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
                crumbs.addView(text("›", 16, muted(), Typeface.BOLD));
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

    private String receiveConnectionLabel() {
        if (receiveSession == null) {
            return string(R.string.connection_disconnected);
        }
        return host.ui().connectionLabel(receiveMetrics);
    }

    private int receiveConnectionColor() {
        if (receiveSession == null) {
            return Color.rgb(148, 163, 184);
        }
        return host.ui().connectionColor(receiveConnectionState());
    }

    private String receiveConnectionState() {
        if (receiveSession == null) {
            return "disconnected";
        }
        return host.ui().connectionState(receiveMetrics);
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
                entry.getValue().setChecked(isPathSelectedForDownload(entry.getKey()));
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
        String summary = string(R.string.selection_summary, selectedRemotePaths.size(), formatBytes(selected.bytes));
        if (!excludedRemotePaths.isEmpty()) {
            summary += " · " + string(R.string.selection_excluded, excludedRemotePaths.size());
        }
        return summary;
    }

    private String remoteTotalSummaryText() {
        return string(R.string.remote_total_summary, remoteFileCount, remoteDirCount, formatBytes(remoteTotalBytes));
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
                || "Receive complete with failures".equals(downloadStatus)
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
            ProgressBar spinner = new ProgressBar(context(), null, android.R.attr.progressBarStyleSmall);
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
            return string(R.string.status_idle);
        }
        if ("Waiting for peer".equals(status)) {
            return string(R.string.connection_waiting_peer);
        }
        if (status.startsWith("Fetching ")) {
            return string(R.string.remote_list_fetching, status.substring("Fetching ".length()));
        }
        if ("Refreshing remote file list".equals(status)) {
            return string(R.string.remote_list_refreshing);
        }
        if ("Remote list ready".equals(status)) {
            return string(R.string.remote_list_ready_status);
        }
        if ("Remote list refreshed".equals(status)) {
            return string(R.string.remote_list_refreshed_status);
        }
        if ("No remote files".equals(status)) {
            return string(R.string.toast_no_remote_files);
        }
        if ("Remote path missing".equals(status)) {
            return string(R.string.folder_missing);
        }
        if ("Remote list failed".equals(status) || "Remote refresh failed".equals(status) || "Failed".equals(status)) {
            return string(R.string.remote_list_failed_status);
        }
        if ("Stopped".equals(status)) {
            return string(R.string.status_stopped);
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
            return string(R.string.status_idle);
        }
        if ("Preparing download".equals(status)) {
            return string(R.string.status_preparing_download);
        }
        if ("Receive complete".equals(status)) {
            return string(R.string.status_receive_complete);
        }
        if ("Receive complete with failures".equals(status)) {
            return string(R.string.status_receive_complete_with_failures);
        }
        if ("Receive failed".equals(status)) {
            return string(R.string.status_receive_failed);
        }
        if ("Stopped".equals(status)) {
            return string(R.string.status_stopped);
        }
        if (status != null && status.startsWith("Receiving ")) {
            return string(R.string.status_receiving, status.substring("Receiving ".length()));
        }
        return status == null ? "" : status;
    }

    private View downloadModeToggle() {
        LinearLayout box = column();
        LinearLayout line = row();
        line.setGravity(Gravity.CENTER_VERTICAL);
        TextView label = text(string(R.string.mode), 12, muted(), Typeface.BOLD);
        line.addView(label, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(30)));
        Button resume = segmentedButton(string(R.string.resume), resumeDownloads);
        setControlEnabled(resume, receiveDownload == null);
        resume.setOnClickListener(v -> {
            if (receiveDownload == null) {
                resumeDownloads = true;
                host.requestRender();
            }
        });
        Button overwrite = segmentedButton(string(R.string.overwrite), !resumeDownloads);
        setControlEnabled(overwrite, receiveDownload == null);
        overwrite.setOnClickListener(v -> {
            if (receiveDownload == null) {
                resumeDownloads = false;
                host.requestRender();
            }
        });
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(30), 1);
        params.setMargins(dp(8), 0, 0, 0);
        line.addView(resume, params);
        line.addView(overwrite, actionParams(dp(30)));
        box.addView(line);
        box.addView(text(string(R.string.resume_overwrite_hint), 12, muted(), Typeface.NORMAL), blockParams(dp(4)));
        return box;
    }

    private boolean canClickRemoteAction() {
        return receiveDownload == null
                && !receivedCompletionRefreshPending
                && remoteListSession == null;
    }

    private boolean canStartReceiveConnection() {
        return receiveSession == null
                && ReceivedFileActionState.canStartNewConnection(
                receiveDownload != null, receivedCompletionRefreshPending);
    }

    private void browseRemotePath(String path) {
        if (!canClickRemoteAction()) {
            return;
        }
        remoteCurrentPath = normalizeRemotePath(path);
        remoteListStatus = remoteFiles.isEmpty() ? "No remote files" : "Remote list ready";
        host.requestRender();
    }

    private List<String> currentDownloadPaths() {
        List<String> paths = new ArrayList<>();
        paths.add(normalizeRemotePath(remoteCurrentPath));
        return paths;
    }

    private void startSelectedDownload() {
        if (receiveEndpoint == null || !receiveEndpoint.startsWith("http://")) {
            remoteListStatus = "Waiting for peer";
            host.toast(R.string.toast_peer_not_connected);
            host.requestRender();
            return;
        }
        if (!"connected".equals(receiveConnectionState())) {
            remoteListStatus = "Waiting for peer";
            host.toast(R.string.toast_peer_not_connected);
            host.requestRender();
            return;
        }
        if (selectedRemotePaths.isEmpty()) {
            host.toast(R.string.toast_select_download_files);
            return;
        }
        if (!ensureDefaultSavePermission()) {
            return;
        }
        List<HttpReceiver.RemoteFile> files = selectedRemoteFiles();
        if (files.isEmpty()) {
            host.toast(R.string.toast_no_remote_files);
            return;
        }
        remoteListStatus = "Remote list ready";
        startReceiveDownload(receiveEndpoint, receiveRunId, files);
        host.requestRender();
    }

    private List<HttpReceiver.RemoteFile> selectedRemoteFiles() {
        List<HttpReceiver.RemoteFile> files = new ArrayList<>();
        for (HttpReceiver.RemoteFile file : remoteFiles) {
            if (isPathSelectedForDownload(file.path)) {
                files.add(file);
            }
        }
        return files;
    }

    private List<HttpReceiver.RemoteFile> filterSelectedRemoteFiles(List<HttpReceiver.RemoteFile> files) {
        List<HttpReceiver.RemoteFile> selected = new ArrayList<>();
        if (files == null || files.isEmpty()) {
            return selected;
        }
        for (HttpReceiver.RemoteFile file : files) {
            if (isPathSelectedForDownload(file.path)) {
                selected.add(file);
            }
        }
        return selected;
    }

    private boolean isPathSelectedForDownload(String path) {
        String normalized = normalizeRemotePath(path);
        int selectedRank = longestPathRuleMatch(normalized, selectedRemotePaths);
        if (selectedRank < 0) {
            return false;
        }
        int excludedRank = longestPathRuleMatch(normalized, excludedRemotePaths);
        return excludedRank < 0 || selectedRank > excludedRank;
    }

    private boolean hasSelectedParent(String path) {
        String parent = parentPath(path);
        return !parent.equals(normalizeRemotePath(path)) && longestPathRuleMatch(parent, selectedRemotePaths) >= 0;
    }

    private int longestPathRuleMatch(String path, Set<String> rules) {
        String normalized = normalizeRemotePath(path);
        int best = -1;
        for (String rule : rules) {
            String item = normalizeRemotePath(rule);
            if (item.isEmpty()) {
                best = Math.max(best, 0);
                continue;
            }
            if (normalized.equals(item) || normalized.startsWith(item + "/")) {
                best = Math.max(best, pathRuleRank(item));
            }
        }
        return best;
    }

    private int pathRuleRank(String path) {
        String normalized = normalizeRemotePath(path);
        if (normalized.isEmpty()) {
            return 0;
        }
        int rank = 1;
        for (int i = 0; i < normalized.length(); i++) {
            if (normalized.charAt(i) == '/') {
                rank++;
            }
        }
        return rank + 1;
    }

    private void selectVisibleRemoteFiles() {
        for (HttpReceiver.RemoteFile file : visibleRemoteFiles()) {
            String path = normalizeRemotePath(file.path);
            excludedRemotePaths.remove(path);
            selectedRemotePaths.add(path);
        }
        updateRemoteSelectionViews();
    }

    private void invertVisibleRemoteFiles() {
        for (HttpReceiver.RemoteFile file : visibleRemoteFiles()) {
            String path = normalizeRemotePath(file.path);
            if (isPathSelectedForDownload(path)) {
                selectedRemotePaths.remove(path);
                if (hasSelectedParent(path)) {
                    excludedRemotePaths.add(path);
                } else {
                    excludedRemotePaths.remove(path);
                }
            } else {
                excludedRemotePaths.remove(path);
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
        box.addView(text(string(R.string.passphrase_hint), 12, muted(), Typeface.NORMAL), blockParams(dp(4)));

        LinearLayout line = row();
        EditText input = new EditText(context());
        input.setSingleLine(true);
        input.setText(receivePassword);
        input.setTextColor(ink());
        input.setTextSize(15);
        input.setHint(string(R.string.passphrase_input_hint));
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
        Button random = secondaryButton(string(R.string.random_passphrase));
        setControlEnabled(random, !locked);
        random.setOnClickListener(v -> randomizePassword());
        Button paste = secondaryButton(string(R.string.paste));
        setControlEnabled(paste, !locked);
        paste.setOnClickListener(v -> pastePassword());
        Button scan = secondaryButton(string(R.string.scan));
        setControlEnabled(scan, !locked);
        scan.setOnClickListener(v -> scanPassword());
        Button qr = secondaryButton(string(R.string.qr));
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
        host.mainHandler().postDelayed(() -> {
            if (receivePasswordVisibilityToken != token) {
                return;
            }
            receivePasswordVisible = false;
            if (input != null && input.isAttachedToWindow()) {
                applyPasswordVisibility(input);
            } else {
                host.requestRender();
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
        host.log("info", "Passphrase randomized");
        host.requestRender();
    }

    private View protocolToggle() {
        LinearLayout box = column();
        CheckBox checkBox = new CheckBox(context());
        checkBox.setText(string(R.string.use_udp_protocol));
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
        TextView hint = text(string(R.string.use_udp_protocol_hint), 12, muted(), Typeface.NORMAL);
        hint.setPadding(dp(4), 0, 0, 0);
        box.addView(hint);
        return box;
    }

    // --- passphrase actions ----------------------------------------------

    private void pastePassword() {
        if (receiveSession != null) {
            return;
        }
        ClipboardManager clipboard = (ClipboardManager) context().getSystemService(Context.CLIPBOARD_SERVICE);
        if (clipboard == null || !clipboard.hasPrimaryClip() || clipboard.getPrimaryClip() == null) {
            host.toast(R.string.toast_clipboard_empty);
            return;
        }
        ClipData clip = clipboard.getPrimaryClip();
        if (clip.getItemCount() == 0) {
            host.toast(R.string.toast_clipboard_empty);
            return;
        }
        CharSequence value = clip.getItemAt(0).coerceToText(context());
        if (value != null) {
            receivePassword = value.toString().trim();
            revealPasswordTemporarily();
            host.log("info", "Passphrase pasted");
            host.requestRender();
        }
    }

    private void showPasswordQr() {
        host.showPassphraseQr(receivePassword.trim());
    }

    private void scanPassword() {
        if (receiveSession != null) {
            return;
        }
        host.scanPassphrase(result -> {
            if (receiveSession != null) {
                return;
            }
            receivePassword = result.trim();
            revealPasswordTemporarily();
            host.log("info", "Passphrase scanned");
            host.requestRender();
        });
    }

    // --- session ----------------------------------------------------------

    private void startP2PReceive() {
        if (!canStartReceiveConnection()) {
            return;
        }
        String passphrase = receivePassword.trim();
        if (passphrase.isEmpty()) {
            host.toast(R.string.toast_passphrase_required);
            return;
        }
        if (Passwords.isWeak(passphrase)) {
            host.toast(R.string.toast_passphrase_weak);
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
        downloadFailedFiles = 0;
        downloadStartedAtMs = 0;
        downloadFinishedAtMs = 0;
        receiveEndpoint = "";
        remoteCurrentPath = "";
        remoteCurrentPathMissing = false;
        remoteFiles.clear();
        selectedRemotePaths.clear();
        excludedRemotePaths.clear();
        remoteFileCount = 0;
        remoteDirCount = 0;
        remoteTotalBytes = 0;
        receiveStatus = "Preparing";
        host.log("info", "Start receiving requested");
        long runId = ++receiveRunId;
        GoncBridge.Session started = host.bridge().startP2PReceive(
                context(), passphrase, receiveUseUdp, callback(runId));
        receiveSession = started;
        if (started != null) {
            receiveDownloadId++;
            receivedTargets.clear();
            receivedTargetCheckId++;
            receivedCompletionRefreshToken++;
        }
        host.refreshForegroundService();
        host.requestRender();
    }

    private GoncBridge.EventCallback callback(long runId) {
        return new GoncBridge.EventCallback() {
            @Override
            public void onEvent(String level, String message) {
                host.mainHandler().post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    host.updateMetricsFromLog(receiveMetrics, message);
                    host.log(level, message);
                    host.requestBackgroundRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onP2PReport(String topic, String side, String status, String network, String mode, String peer, long timestamp, long pid) {
                host.mainHandler().post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    host.updateMetricsFromReport(receiveMetrics, topic, status, network, mode, peer);
                    host.requestRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onTraffic(String side, long inBytes, long outBytes, double inBps, double outBps, long elapsed, long connCount, boolean isFinal) {
                host.mainHandler().post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    host.updateMetricsFromTraffic(receiveMetrics, inBytes, outBytes, inBps, outBps);
                    host.requestBackgroundRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onReady(String endpoint) {
                host.mainHandler().post(() -> {
                    if (!isActiveRun(runId)) {
                        return;
                    }
                    receiveStatus = "Ready";
                    receiveMetrics.p2pStatus = "connected";
                    host.log("info", "Ready: " + endpoint);
                    loadRemoteFiles(endpoint, runId);
                    host.requestRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onStopped() {
                host.mainHandler().post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    receiveSession = null;
                    receiveStatus = "Idle";
                    stopReceiveWorkers();
                    receiveMetrics.markStopped();
                    host.refreshForegroundService();
                    host.log("warn", "Session stopped");
                    host.requestRender();
                });
            }

            @Override
            public void onError(Throwable error) {
                host.mainHandler().post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    receiveSession = null;
                    receiveStatus = "Error";
                    stopReceiveWorkers();
                    remoteListStatus = "Failed";
                    receiveMetrics.p2pStatus = "error";
                    host.refreshForegroundService();
                    host.log("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    host.requestRender();
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
        host.refreshForegroundService();
        host.log("warn", "Receive stop requested");
        host.requestRender();
    }

    private void loadRemoteFiles(String endpoint, long runId) {
        loadRemoteFiles(endpoint, runId, "", true, true);
    }

    private void refreshRemoteFiles(String subPath) {
        if (receiveEndpoint == null || !receiveEndpoint.startsWith("http://")) {
            remoteListStatus = "Waiting for peer";
            host.toast(R.string.toast_peer_not_connected);
            host.requestRender();
            return;
        }
        if (!"connected".equals(receiveConnectionState())) {
            remoteListStatus = "Waiting for peer";
            host.toast(R.string.toast_peer_not_connected);
            host.requestRender();
            return;
        }
        loadRemoteFiles(receiveEndpoint, receiveRunId, subPath, true, false);
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
            excludedRemotePaths.clear();
        }
        remoteListSession = HttpReceiver.startList(endpoint, targetPath, new HttpReceiver.ListCallback() {
            @Override
            public void onList(List<HttpReceiver.RemoteFile> files, int fileCount, int dirCount, long totalBytes, boolean missing) {
                host.mainHandler().post(() -> {
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
                    host.refreshForegroundService();
                    if (remoteCurrentPathMissing) {
                        host.log("warn", "Remote path missing " + displayRemotePath(targetPath));
                    } else {
                        host.log("info", "Remote list ready " + displayRemotePath(targetPath) + ": " + fileCount + " file(s), " + dirCount + " folder(s), " + formatBytes(totalBytes));
                    }
                    host.requestRender();
                    refreshReceivedTargets(runId, targetPath);
                });
            }

            @Override
            public void onError(Throwable error) {
                host.mainHandler().post(() -> {
                    if (!isCurrentRun(runId)) {
                        return;
                    }
                    remoteListSession = null;
                    remoteListStatus = "Remote list failed";
                    host.refreshForegroundService();
                    host.log("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    host.toast(R.string.toast_remote_refresh_failed);
                    host.requestRender();
                });
            }
        });
        host.refreshForegroundService();
        host.requestRender();
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

    private void refreshAndStartDownload(List<String> paths, boolean applySelectionRules) {
        if (receiveEndpoint == null || !receiveEndpoint.startsWith("http://")) {
            remoteListStatus = "Waiting for peer";
            host.toast(R.string.toast_peer_not_connected);
            host.requestRender();
            return;
        }
        if (!"connected".equals(receiveConnectionState())) {
            remoteListStatus = "Waiting for peer";
            host.toast(R.string.toast_peer_not_connected);
            host.requestRender();
            return;
        }
        if (paths == null || paths.isEmpty()) {
            host.toast(R.string.toast_select_download_files);
            return;
        }
        if (!ensureDefaultSavePermission()) {
            return;
        }
        if (currentDirectoryFiles().isEmpty() && paths.size() == 1 && normalizeRemotePath(paths.get(0)).equals(normalizeRemotePath(remoteCurrentPath))) {
            host.toast(R.string.toast_no_remote_files);
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
                host.mainHandler().post(() -> {
                    if (!isCurrentRun(receiveRunId)) {
                        return;
                    }
                    remoteListSession = null;
                    mergeRemoteFiles(normalizedPaths, files);
                    recalculateRemoteFileSummary();
                    remoteCurrentPathMissing = missing && normalizedPaths.size() == 1 && normalizeRemotePath(remoteCurrentPath).equals(normalizedPaths.get(0)) && !remoteCurrentPath.isEmpty();
                    remoteListStatus = remoteCurrentPathMissing ? "Remote path missing" : (files.isEmpty() ? "No remote files" : "Remote list refreshed");
                    host.refreshForegroundService();
                    List<HttpReceiver.RemoteFile> downloadFiles = applySelectionRules ? filterSelectedRemoteFiles(files) : files;
                    if (downloadFiles.isEmpty()) {
                        downloadStatus = "Idle";
                        host.toast(R.string.toast_no_remote_files);
                        host.requestRender();
                        return;
                    }
                    host.log("info", "Remote list refreshed before download: " + fileCount + " file(s), " + dirCount + " folder(s), " + formatBytes(totalBytes));
                    startReceiveDownload(receiveEndpoint, receiveRunId, downloadFiles);
                    host.requestRender();
                });
            }

            @Override
            public void onError(Throwable error) {
                host.mainHandler().post(() -> {
                    if (!isCurrentRun(receiveRunId)) {
                        return;
                    }
                    remoteListSession = null;
                    remoteListStatus = "Remote refresh failed";
                    downloadStatus = "Idle";
                    host.refreshForegroundService();
                    host.log("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    host.toast(R.string.toast_remote_refresh_failed);
                    host.requestRender();
                });
            }
        });
        host.refreshForegroundService();
        host.requestRender();
    }

    private void startReceiveDownload(String endpoint, long runId, List<HttpReceiver.RemoteFile> files) {
        if (endpoint == null || !endpoint.startsWith("http://")) {
            return;
        }
        if (files == null || files.isEmpty()) {
            host.toast(R.string.toast_select_download_files);
            return;
        }
        if (!ensureDefaultSavePermission()) {
            return;
        }
        if (receiveDownload != null || receivedCompletionRefreshPending) {
            return;
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
        downloadFailedFiles = 0;
        downloadStartedAtMs = System.currentTimeMillis();
        downloadFinishedAtMs = 0;
        long downloadId = ++receiveDownloadId;
        DownloadTerminalHolder terminal = new DownloadTerminalHolder();
        HttpReceiver.Session startedDownload = HttpReceiver.start(context(), endpoint, saveTreeUri, files, resumeDownloads, new HttpReceiver.Callback() {
            @Override
            public void onProgress(int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current) {
                queueDownloadProgress(runId, doneFiles, totalFiles, doneBytes, totalBytes, networkBytes, bytesPerSecond, current);
            }

            @Override
            public void onComplete(int totalFiles, long doneBytes, long totalBytes, long networkBytes, int skippedFiles, int resumedFiles, List<HttpReceiver.DownloadFailure> failures) {
                terminal.result = DownloadTerminalResult.completed(
                        totalFiles, doneBytes, totalBytes, networkBytes,
                        skippedFiles, resumedFiles, failures);
            }

            @Override
            public void onError(Throwable error) {
                terminal.result = DownloadTerminalResult.failed(error);
            }
        });
        receiveDownload = startedDownload;
        startedDownload.onTerminated(() -> host.mainHandler().post(
                () -> finishReceiveDownload(runId, downloadId, terminal.result)));
        host.refreshForegroundService();
    }

    private void finishReceiveDownload(
            long runId, long downloadId, DownloadTerminalResult result) {
        if (!isCurrentRun(runId) || shutdown
                || !ReceivedFileActionState.shouldBeginTerminalRefresh(
                receiveDownloadId, downloadId, receiveDownload != null,
                receivedCompletionRefreshPending)) {
            return;
        }
        long completionRefreshToken = ++receivedCompletionRefreshToken;
        receivedCompletionRefreshPending = true;
        receiveDownload = null;
        synchronized (downloadProgressLock) {
            pendingDownloadProgress = null;
            downloadProgressApplyPending = false;
        }
        downloadBytesPerSecond = 0;
        downloadFinishedAtMs = System.currentTimeMillis();
        receiveMetrics.inBps = 0;
        receiveMetrics.outBps = 0;
        receiveMetrics.lastTrafficMs = 0;

        if (result == null) {
            downloadStatus = "Stopped";
            host.log("warn", "Receive download stopped");
        } else if (result.error != null) {
            downloadStatus = "Receive failed";
            host.log("error", result.error.getMessage() == null
                    ? result.error.toString() : result.error.getMessage());
        } else {
            downloadDoneFiles = result.totalFiles;
            downloadTotalFiles = result.totalFiles;
            downloadDoneBytes = result.doneBytes;
            downloadTotalBytes = result.totalBytes;
            downloadNetworkBytes = result.networkBytes;
            downloadSkippedFiles = result.skippedFiles;
            downloadResumedFiles = result.resumedFiles;
            int failedFiles = result.failures.size();
            downloadFailedFiles = failedFiles;
            downloadStatus = failedFiles > 0
                    ? "Receive complete with failures" : "Receive complete";
            if (failedFiles > 0) {
                host.log("error", "Receive completed with " + failedFiles
                        + " failed file(s): " + result.totalFiles + " total, "
                        + HttpReceiver.formatBytes(result.doneBytes) + " / "
                        + HttpReceiver.formatBytes(result.totalBytes)
                        + ", skipped " + result.skippedFiles
                        + ", resumed " + result.resumedFiles);
                for (HttpReceiver.DownloadFailure failure : result.failures) {
                    host.log("error", "Receive failed: " + failure.path
                            + " - " + failure.message);
                }
            } else {
                host.log("info", "Receive complete: " + result.totalFiles
                        + " file(s), " + HttpReceiver.formatBytes(result.totalBytes)
                        + ", skipped " + result.skippedFiles
                        + ", resumed " + result.resumedFiles);
            }
        }
        refreshReceivedTargets(runId, remoteCurrentPath, completionRefreshToken);
        host.refreshForegroundService();
        host.requestRender();
    }

    private void queueDownloadProgress(long runId, int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current) {
        synchronized (downloadProgressLock) {
            pendingDownloadProgress = new DownloadProgressSnapshot(runId, doneFiles, totalFiles, doneBytes, totalBytes, networkBytes, bytesPerSecond, current);
            if (downloadProgressApplyPending) {
                return;
            }
            downloadProgressApplyPending = true;
        }
        host.mainHandler().post(this::applyLatestDownloadProgress);
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
        host.refreshForegroundService();
    }

    private boolean ensureDefaultSavePermission() {
        if (saveTreeUri != null) {
            return true;
        }
        return host.ensureLegacyStoragePermission();
    }

    private void stopReceiveDownload() {
        if (receiveDownload != null) {
            receiveDownload.stop();
        }
        synchronized (downloadProgressLock) {
            pendingDownloadProgress = null;
            downloadProgressApplyPending = false;
        }
        downloadStatus = "Stopping";
        downloadBytesPerSecond = 0;
        receiveMetrics.inBps = 0;
        receiveMetrics.outBps = 0;
        receiveMetrics.lastTrafficMs = 0;
        host.refreshForegroundService();
        host.log("warn", "Receive download stop requested");
        host.requestRender();
    }

    private void stopReceiveWorkers() {
        if (remoteListSession != null) {
            remoteListSession.stop();
            remoteListSession = null;
        }
        if (receiveDownload != null) {
            receiveDownload.stop();
        }
        downloadBytesPerSecond = 0;
        receiveMetrics.inBps = 0;
        receiveMetrics.outBps = 0;
        receiveMetrics.lastTrafficMs = 0;
    }

    // --- download progress rendering -------------------------------------

    private void renderDownloadProgress() {
        if (host.isAutoRenderPaused()) {
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
        host.mainHandler().postDelayed(() -> {
            downloadProgressRenderPending = false;
            if (receiveDownload == null || host.isAutoRenderPaused()) {
                return;
            }
            lastDownloadProgressRenderMs = System.currentTimeMillis();
            updateDownloadProgressViews();
        }, Math.max(50, 350 - elapsed));
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
        String detailText = string(R.string.progress_detail, formatPercent(done, total), downloadDoneFiles, downloadTotalFiles, formatBytes(done), formatBytes(total));
        if (downloadSkippedFiles > 0 || downloadResumedFiles > 0) {
            detailText = string(R.string.progress_detail_extra, detailText, downloadSkippedFiles, downloadResumedFiles);
        }
        if (downloadFailedFiles > 0) {
            detailText = string(R.string.progress_detail_failed_extra, detailText, downloadFailedFiles);
        }
        return detailText;
    }

    private String downloadProgressSummary(long done, long total) {
        if ("Receive complete with failures".equals(downloadStatus)) {
            return string(R.string.progress_completed_with_failures_summary,
                    downloadFailedFiles,
                    formatDuration(downloadElapsedSeconds()),
                    formatRate(downloadAverageBytesPerSecond()));
        }
        if ("Receive complete".equals(downloadStatus)) {
            return string(R.string.progress_completed_summary,
                    formatDuration(downloadElapsedSeconds()),
                    formatRate(downloadAverageBytesPerSecond()));
        }
        if ("Stopped".equals(downloadStatus)) {
            return string(R.string.progress_stopped_summary, formatDuration(downloadElapsedSeconds()));
        }
        double speedValue = currentDownloadSpeed();
        String speedText = string(R.string.progress_speed, formatRate(speedValue));
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

    private String formatRemaining(long done, long total, double bytesPerSecond) {
        if (total <= 0 || done >= total || receiveDownload == null) {
            return "";
        }
        if (bytesPerSecond <= 1) {
            return string(R.string.progress_remaining_estimating);
        }
        long seconds = Math.max(1, (long) Math.ceil((total - done) / bytesPerSecond));
        return string(R.string.progress_remaining, formatDuration(seconds));
    }

    private boolean isCurrentRun(long runId) {
        return receiveRunId == runId;
    }

    private boolean isActiveRun(long runId) {
        return receiveRunId == runId && receiveSession != null;
    }

    // --- delegates to shared services ------------------------------------

    private Context context() {
        return host.context();
    }

    private String string(int resId) {
        return host.context().getString(resId);
    }

    private String string(int resId, Object... args) {
        return host.context().getString(resId, args);
    }

    private String displayStatus(TransferMetrics metrics) {
        return host.ui().displayStatus(metrics);
    }

    private String routeLabel(String mode) {
        return host.ui().routeLabel(mode);
    }

    private String appendRoute(String label, String route) {
        return host.ui().appendRoute(label, route);
    }

    private int dp(int value) {
        return host.ui().dp(value);
    }

    private int ink() {
        return host.ui().ink();
    }

    private int muted() {
        return host.ui().muted();
    }

    private android.graphics.drawable.GradientDrawable rounded(int color, int radius, int strokeColor, int strokeWidthDp) {
        return host.ui().rounded(color, radius, strokeColor, strokeWidthDp);
    }

    private LinearLayout card() {
        return host.ui().card();
    }

    private LinearLayout row() {
        return host.ui().row();
    }

    private LinearLayout column() {
        return host.ui().column();
    }

    private LinearLayout.LayoutParams blockParams() {
        return host.ui().blockParams();
    }

    private LinearLayout.LayoutParams blockParams(int topMargin) {
        return host.ui().blockParams(topMargin);
    }

    private LinearLayout.LayoutParams dividerParams(int topMargin) {
        return host.ui().dividerParams(topMargin);
    }

    private LinearLayout.LayoutParams actionParams() {
        return host.ui().actionParams();
    }

    private LinearLayout.LayoutParams actionParams(int height) {
        return host.ui().actionParams(height);
    }

    private TextView text(String value, int sp, int color, int style) {
        return host.ui().text(value, sp, color, style);
    }

    private TextView sectionTitle(String title) {
        return host.ui().sectionTitle(title);
    }

    private View sectionBoundaryTitle(String title, boolean separated) {
        return host.ui().sectionBoundaryTitle(title, separated);
    }

    private View sectionDivider() {
        return host.ui().sectionDivider();
    }

    private Button primaryButton(String label) {
        return host.ui().primaryButton(label);
    }

    private Button outlineButton(String label) {
        return host.ui().outlineButton(label);
    }

    private Button dangerButton(String label) {
        return host.ui().dangerButton(label);
    }

    private Button warningButton(String label) {
        return host.ui().warningButton(label);
    }

    private Button secondaryButton(String label) {
        return host.ui().secondaryButton(label);
    }

    private Button quietTouchButton(String label) {
        return host.ui().quietTouchButton(label);
    }

    private Button segmentedButton(String label, boolean active) {
        return host.ui().segmentedButton(label, active);
    }

    private void setControlEnabled(View view, boolean enabled) {
        host.ui().setControlEnabled(view, enabled);
    }

    private String formatBytes(long value) {
        return host.ui().formatBytes(value);
    }

    private String formatRate(double bytesPerSecond) {
        return host.ui().formatRate(bytesPerSecond);
    }

    private String formatPercent(long done, long total) {
        return host.ui().formatPercent(done, total);
    }

    private int progressValue(long done, long total) {
        return host.ui().progressValue(done, total);
    }

    private String formatDuration(long seconds) {
        return host.ui().formatDuration(seconds);
    }

    private String formatTimestamp(long epochMillis) {
        return java.time.Instant.ofEpochMilli(epochMillis)
                .atZone(java.time.ZoneId.systemDefault())
                .format(java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm"));
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

    private static final class DownloadTerminalResult {
        final int totalFiles;
        final long doneBytes;
        final long totalBytes;
        final long networkBytes;
        final int skippedFiles;
        final int resumedFiles;
        final List<HttpReceiver.DownloadFailure> failures;
        final Throwable error;

        private DownloadTerminalResult(
                int totalFiles,
                long doneBytes,
                long totalBytes,
                long networkBytes,
                int skippedFiles,
                int resumedFiles,
                List<HttpReceiver.DownloadFailure> failures,
                Throwable error) {
            this.totalFiles = totalFiles;
            this.doneBytes = doneBytes;
            this.totalBytes = totalBytes;
            this.networkBytes = networkBytes;
            this.skippedFiles = skippedFiles;
            this.resumedFiles = resumedFiles;
            this.failures = failures == null
                    ? Collections.emptyList() : new ArrayList<>(failures);
            this.error = error;
        }

        static DownloadTerminalResult completed(
                int totalFiles,
                long doneBytes,
                long totalBytes,
                long networkBytes,
                int skippedFiles,
                int resumedFiles,
                List<HttpReceiver.DownloadFailure> failures) {
            return new DownloadTerminalResult(
                    totalFiles, doneBytes, totalBytes, networkBytes,
                    skippedFiles, resumedFiles, failures, null);
        }

        static DownloadTerminalResult failed(Throwable error) {
            return new DownloadTerminalResult(
                    0, 0, 0, 0, 0, 0, Collections.emptyList(), error);
        }
    }

    private static final class DownloadTerminalHolder {
        volatile DownloadTerminalResult result;
    }
}
