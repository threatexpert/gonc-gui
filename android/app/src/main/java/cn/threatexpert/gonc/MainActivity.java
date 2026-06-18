package cn.threatexpert.gonc;

import android.app.Activity;
import android.app.Dialog;
import android.Manifest;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.ContentResolver;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.provider.DocumentsContract;
import android.provider.OpenableColumns;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ScrollView;
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

public final class MainActivity extends Activity {
    private static final String SOURCE_URL = "https://github.com/threatexpert/gonc-gui";
    private static final int REQUEST_OPEN_DOCUMENT = 1001;
    private static final int REQUEST_SCAN_QR = 1002;
    private static final int REQUEST_OPEN_SAVE_TREE = 1003;
    private static final int REQUEST_CAMERA_PERMISSION = 1004;
    private static final int REQUEST_OPEN_SEND_TREE = 1005;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final List<ShareItem> shareItems = new ArrayList<>();
    private final List<String> logs = new ArrayList<>();
    private final GoncBridge bridge = new MobileGoncBridge();
    private final TransferMetrics sendMetrics = new TransferMetrics();
    private final TransferMetrics receiveMetrics = new TransferMetrics();

    private LinearLayout root;
    private boolean sendMode = true;
    private boolean useUdp;
    private boolean activityExpanded;
    private boolean scanningForSendMode;
    private boolean sendPasswordVisible;
    private boolean receivePasswordVisible;
    private int sendPasswordVisibilityToken;
    private int receivePasswordVisibilityToken;
    private String sendPassword = Passwords.generate();
    private String receivePassword = "";
    private Uri saveTreeUri;
    private String saveLocationLabel;
    private String receiveEndpoint = "";
    private String remoteListStatus = "Idle";
    private String remoteCurrentPath = "";
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
    private double downloadBytesPerSecond;
    private int downloadSkippedFiles;
    private int downloadResumedFiles;
    private String sendStatus = "Idle";
    private String receiveStatus = "Idle";
    private GoncBridge.Session sendSession;
    private GoncBridge.Session receiveSession;
    private HttpReceiver.Session remoteListSession;
    private HttpReceiver.Session receiveDownload;
    private long sendRunId;
    private long receiveRunId;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        saveLocationLabel = getString(R.string.not_selected);
        buildRoot();
        handleIncomingIntent(getIntent());
        render();
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleIncomingIntent(intent);
        render();
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == REQUEST_SCAN_QR) {
            String result = data == null ? null : data.getStringExtra(Intents.Scan.RESULT);
            if (result != null && !result.trim().isEmpty()) {
                if (isPasswordLocked(scanningForSendMode)) {
                    return;
                }
                setPassword(scanningForSendMode, result.trim());
                revealPasswordTemporarily(scanningForSendMode);
                appendLog("info", "Passphrase scanned");
                render();
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
        }
    }

    @Override
    protected void onDestroy() {
        if (sendSession != null) {
            sendSession.stop();
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
        if (isAnySessionRunning()) {
            root.addView(keepScreenNotice());
        }
        if (sendMode) {
            root.addView(sendPanel());
        } else {
            root.addView(receivePanel());
            if (!receiveEndpoint.trim().isEmpty() || !remoteFiles.isEmpty() || !"Idle".equals(remoteListStatus)) {
                root.addView(remoteFilesPanel());
            }
        }
        root.addView(logPanel());
    }

    private View header() {
        LinearLayout header = row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        header.setPadding(0, 0, 0, dp(12));

        TextView mark = text("G", 22, Color.WHITE, Typeface.BOLD);
        mark.setGravity(Gravity.CENTER);
        mark.setBackground(rounded(Color.rgb(27, 143, 106), dp(8), 0, 0));
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
        LinearLayout box = row();
        box.setPadding(dp(4), dp(4), dp(4), dp(4));
        box.setBackground(rounded(Color.WHITE, dp(8), Color.rgb(216, 226, 238), 1));

        Button send = modeButton(getString(R.string.send_files), sendMode);
        send.setOnClickListener(v -> {
            sendMode = true;
            render();
        });
        Button receive = modeButton(getString(R.string.receive_files), !sendMode);
        receive.setOnClickListener(v -> {
            sendMode = false;
            render();
        });
        box.addView(send, new LinearLayout.LayoutParams(0, dp(42), 1));
        box.addView(receive, new LinearLayout.LayoutParams(0, dp(42), 1));
        box.setLayoutParams(blockParams());
        return box;
    }

    private View keepScreenNotice() {
        TextView notice = text(getString(R.string.screen_stays_on), 13, Color.rgb(15, 111, 83), Typeface.BOLD);
        notice.setGravity(Gravity.CENTER_VERTICAL);
        notice.setPadding(dp(12), 0, dp(12), 0);
        notice.setBackground(rounded(Color.rgb(223, 243, 236), dp(8), Color.rgb(143, 211, 189), 1));
        notice.setLayoutParams(blockParams());
        return notice;
    }

    private View metricBox(String label, String value) {
        LinearLayout box = column();
        box.setPadding(dp(10), dp(8), dp(10), dp(8));
        box.setBackground(rounded(Color.rgb(251, 253, 255), dp(7), Color.rgb(226, 232, 240), 1));
        box.addView(text(label, 11, muted(), Typeface.BOLD));
        TextView content = text(value, 14, ink(), Typeface.BOLD);
        content.setSingleLine(true);
        box.addView(content);
        return box;
    }

    private View sendPanel() {
        LinearLayout card = card();
        card.addView(sectionTitle(getString(R.string.send_files_title)));

        if (shareItems.isEmpty()) {
            TextView empty = text(getString(R.string.no_files_selected), 14, muted(), Typeface.NORMAL);
            empty.setGravity(Gravity.CENTER);
            empty.setPadding(dp(12), dp(22), dp(12), dp(22));
            empty.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(143, 168, 195), 1));
            card.addView(empty, blockParams());
        } else {
            for (ShareItem item : shareItems) {
                card.addView(fileRow(item));
            }
        }

        LinearLayout actions = row();
        Button add = secondaryButton(getString(R.string.add_files));
        add.setOnClickListener(v -> openDocumentPicker());
        Button addFolder = secondaryButton(getString(R.string.add_folder));
        addFolder.setOnClickListener(v -> openSendFolderPicker());
        Button clear = secondaryButton(getString(R.string.clear));
        clear.setOnClickListener(v -> {
            shareItems.clear();
            appendLog("info", "Shared file list cleared");
            render();
        });
        actions.addView(add, new LinearLayout.LayoutParams(0, dp(42), 1));
        actions.addView(addFolder, new LinearLayout.LayoutParams(0, dp(42), 1));
        actions.addView(clear, new LinearLayout.LayoutParams(0, dp(42), 1));
        card.addView(actions, blockParams());

        card.addView(passwordField(getString(R.string.passphrase), true));
        card.addView(protocolToggle());

        Button primary = sendSession == null ? primaryButton(getString(R.string.start_sharing)) : dangerButton(getString(R.string.stop_sharing));
        primary.setOnClickListener(v -> {
            if (sendSession == null) {
                startP2PShare();
            } else {
                stopSession(true);
            }
        });
        card.addView(primary, blockParams(dp(12)));
        return card;
    }

    private View receivePanel() {
        LinearLayout card = card();
        card.addView(sectionTitle(getString(R.string.receive_files_title)));
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
        card.addView(passwordField(getString(R.string.passphrase), false));
        card.addView(protocolToggle());

        Button primary = receiveSession == null ? primaryButton(getString(R.string.connect)) : dangerButton(getString(R.string.disconnect));
        primary.setOnClickListener(v -> {
            if (receiveSession == null) {
                startP2PReceive();
            } else {
                stopSession(false);
            }
        });
        card.addView(primary, blockParams(dp(12)));
        return card;
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
        box.addView(line);

        long total = Math.max(downloadTotalBytes, 0);
        long done = Math.max(downloadDoneBytes, 0);
        String detailText = getString(R.string.progress_detail, formatPercent(done, total), downloadDoneFiles, downloadTotalFiles, formatBytes(done), formatBytes(total));
        if (downloadSkippedFiles > 0 || downloadResumedFiles > 0) {
            detailText = getString(R.string.progress_detail_extra, detailText, downloadSkippedFiles, downloadResumedFiles);
        }
        TextView detail = text(detailText, 13, muted(), Typeface.BOLD);
        detail.setSingleLine(false);
        box.addView(detail, blockParams(dp(8)));

        TextView speed = text(formatRate(currentDownloadSpeed()), 13, Color.rgb(15, 111, 83), Typeface.BOLD);
        box.addView(speed, blockParams(dp(4)));
        return box;
    }

    private View remoteFilesPanel() {
        LinearLayout card = card();
        card.addView(sectionTitle(getString(R.string.remote_files)));
        card.addView(receiveConnectionStatusView(), blockParams(0));
        card.addView(downloadModeToggle(), blockParams(dp(8)));

        LinearLayout actions = row();
        if (receiveDownload != null) {
            Button stop = dangerButton(getString(R.string.stop_download));
            stop.setOnClickListener(v -> stopReceiveDownload());
            actions.addView(stop, new LinearLayout.LayoutParams(0, dp(42), 1));
        } else {
            Button all = primaryButton(getString(R.string.receive_all));
            all.setEnabled(canClickRemoteAction());
            all.setOnClickListener(v -> refreshAndStartDownload(currentDownloadPaths()));
            Button selected = primaryButton(getString(R.string.download_selected));
            selected.setEnabled(canClickRemoteAction());
            selected.setOnClickListener(v -> refreshAndStartDownload(new ArrayList<>(selectedRemotePaths)));
            actions.addView(all, new LinearLayout.LayoutParams(0, dp(42), 1));
            actions.addView(selected, actionParams(dp(42)));
        }
        card.addView(actions, blockParams(dp(8)));

        if (!"Idle".equals(downloadStatus)) {
            card.addView(receiveProgressContent(), blockParams(dp(8)));
        }

        card.addView(remoteFilesSummaryView(), blockParams(dp(10)));

        LinearLayout pathRow = row();
        pathRow.setGravity(Gravity.CENTER_VERTICAL);
        TextView path = text(displayRemotePath(remoteCurrentPath), 13, ink(), Typeface.BOLD);
        path.setSingleLine(true);
        pathRow.addView(path, new LinearLayout.LayoutParams(0, dp(38), 1));
        Button refresh = ghostButton(getString(R.string.refresh));
        refresh.setEnabled(canClickRemoteAction());
        refresh.setOnClickListener(v -> refreshRemoteFiles(remoteCurrentPath));
        pathRow.addView(refresh, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        card.addView(pathRow, blockParams(dp(8)));
        card.addView(remoteListStatusView(), blockParams(dp(4)));

        if (remoteFiles.isEmpty()) {
            card.addView(text(getString(R.string.remote_list_waiting), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
            return card;
        }

        List<HttpReceiver.RemoteFile> visible = visibleRemoteFiles();
        LinearLayout list = column();
        if (!remoteCurrentPath.isEmpty()) {
            list.addView(parentDirectoryRow());
        }
        if (visible.isEmpty()) {
            list.addView(text(getString(R.string.folder_empty), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
        } else {
            for (HttpReceiver.RemoteFile file : visible) {
                list.addView(remoteFileRow(file));
            }
        }
        ScrollView listScroll = new InterceptingScrollView(this);
        listScroll.setFillViewport(false);
        listScroll.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(226, 232, 240), 1));
        listScroll.addView(list, new ScrollView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        LinearLayout.LayoutParams listParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(340));
        listParams.setMargins(0, dp(8), 0, 0);
        card.addView(listScroll, listParams);
        return card;
    }

    private View parentDirectoryRow() {
        LinearLayout row = row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(12), dp(10), dp(12), dp(10));
        row.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));

        LinearLayout labels = column();
        TextView name = text(getString(R.string.parent_directory), 13, Color.rgb(32, 101, 165), Typeface.BOLD);
        labels.addView(name);
        labels.addView(text(parentPath(remoteCurrentPath).isEmpty() ? "/" : displayRemotePath(parentPath(remoteCurrentPath)), 12, muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row.setLayoutParams(blockParams(dp(6)));
        row.setOnClickListener(v -> {
            if (receiveDownload == null) {
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
        checkBox.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (isChecked) {
                selectedRemotePaths.add(normalizedPath);
            } else {
                selectedRemotePaths.remove(normalizedPath);
            }
            render();
        });
        row.addView(checkBox, new LinearLayout.LayoutParams(dp(42), dp(42)));

        LinearLayout labels = column();
        TextView name = text(baseName(normalizedPath), 13, file.isDir ? Color.rgb(32, 101, 165) : ink(), Typeface.BOLD);
        name.setSingleLine(true);
        labels.addView(name);
        labels.addView(text(file.isDir ? getString(R.string.folder) : formatBytes(file.size), 12, muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row.setLayoutParams(blockParams(dp(6)));
        row.setOnClickListener(v -> {
            if (file.isDir && receiveDownload == null) {
                browseRemotePath(normalizedPath);
            }
        });
        return row;
    }

    private View receiveConnectionStatusView() {
        LinearLayout box = row();
        box.setGravity(Gravity.CENTER_VERTICAL);
        box.setPadding(dp(10), dp(8), dp(10), dp(8));
        box.setBackground(rounded(Color.rgb(248, 251, 255), dp(8), Color.rgb(226, 232, 240), 1));

        View dot = new View(this);
        dot.setBackground(rounded(receiveConnectionColor(), dp(6), 0, 0));
        box.addView(dot, new LinearLayout.LayoutParams(dp(12), dp(12)));

        LinearLayout labels = column();
        labels.addView(text(receiveConnectionLabel(), 14, ink(), Typeface.BOLD));
        TextView detail = text(receiveConnectionDetail(), 12, muted(), Typeface.NORMAL);
        detail.setSingleLine(true);
        labels.addView(detail);
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
        if ("failed".equals(state)) {
            return getString(R.string.connection_failed);
        }
        if ("connected".equals(state)) {
            return getString(R.string.connection_connected);
        }
        if ("waiting".equals(state)) {
            return getString(R.string.connection_waiting_peer);
        }
        return getString(R.string.connection_connecting);
    }

    private String receiveConnectionDetail() {
        if (receiveSession == null) {
            return "-";
        }
        String peer = emptyDash(receiveMetrics.peer);
        String address = "-".equals(peer) ? emptyDash(receiveEndpoint) : peer;
        if ("-".equals(address)) {
            return getString(R.string.connection_address_pending);
        }
        String network = emptyDash(receiveMetrics.network);
        return "-".equals(network) ? address : network + "  " + address;
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
        if ("ready".equals(clean) || "idle".equals(clean) || "disconnected".equals(clean)) {
            return "waiting";
        }
        return "connecting";
    }

    private View remoteFilesSummaryView() {
        String summary = getString(R.string.files_summary, remoteFileCount, remoteDirCount, formatBytes(remoteTotalBytes), selectedRemotePaths.size());
        TextView view = text(summary, 13, muted(), Typeface.BOLD);
        view.setSingleLine(false);
        return view;
    }

    private View remoteListStatusView() {
        TextView view = text(displayRemoteListStatus(), 12, remoteListStatusColor(), Typeface.BOLD);
        view.setSingleLine(false);
        return view;
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
        if ("Remote list failed".equals(status) || "Remote refresh failed".equals(status) || "Failed".equals(status)) {
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
        LinearLayout box = row();
        box.setGravity(Gravity.CENTER_VERTICAL);
        TextView label = text(getString(R.string.mode), 12, muted(), Typeface.BOLD);
        box.addView(label, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(30)));
        Button resume = segmentedButton(getString(R.string.resume), resumeDownloads);
        resume.setOnClickListener(v -> {
            if (receiveDownload == null) {
                resumeDownloads = true;
                render();
            }
        });
        Button overwrite = segmentedButton(getString(R.string.overwrite), !resumeDownloads);
        overwrite.setOnClickListener(v -> {
            if (receiveDownload == null) {
                resumeDownloads = false;
                render();
            }
        });
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(30), 1);
        params.setMargins(dp(8), 0, 0, 0);
        box.addView(resume, params);
        box.addView(overwrite, actionParams(dp(30)));
        return box;
    }

    private Button segmentedButton(String label, boolean active) {
        Button button = button(label, active ? Color.rgb(40, 112, 216) : Color.rgb(82, 101, 125), active ? Color.rgb(232, 240, 252) : Color.TRANSPARENT);
        button.setTextSize(12);
        button.setPadding(dp(6), 0, dp(6), 0);
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        return button;
    }

    private boolean canClickRemoteAction() {
        return receiveDownload == null && remoteListSession == null;
    }

    private void browseRemotePath(String path) {
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

    private View passwordField(String label, boolean sender) {
        boolean locked = isPasswordLocked(sender);
        LinearLayout box = column();
        box.addView(text(label, 13, Color.rgb(64, 81, 105), Typeface.BOLD));

        LinearLayout line = row();
        EditText input = new EditText(this);
        input.setSingleLine(true);
        input.setText(passwordFor(sender));
        input.setTextColor(ink());
        input.setTextSize(15);
        input.setPadding(dp(12), 0, dp(12), 0);
        input.setBackground(rounded(Color.WHITE, dp(6), Color.rgb(203, 215, 230), 1));
        input.setEnabled(!locked);
        input.setTextColor(ink());
        applyPasswordVisibility(input, sender);
        input.addTextChangedListener(new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
                if (isPasswordLocked(sender)) {
                    return;
                }
                setPassword(sender, s.toString());
                revealPasswordTemporarily(sender, input);
            }

            @Override
            public void afterTextChanged(Editable s) {
            }
        });
        line.addView(input, new LinearLayout.LayoutParams(0, dp(46), 1));

        box.addView(line, blockParams(dp(8)));

        LinearLayout actions = row();
        if (sender) {
            Button change = secondaryButton(getString(R.string.change));
            change.setEnabled(!locked);
            change.setOnClickListener(v -> {
                if (isPasswordLocked(true)) {
                    return;
                }
                setPassword(true, Passwords.generate());
                revealPasswordTemporarily(true);
                appendLog("info", "Passphrase changed");
                render();
            });
            Button copy = secondaryButton(getString(R.string.copy));
            copy.setOnClickListener(v -> copyPassword(true));
            Button scan = secondaryButton(getString(R.string.scan));
            scan.setEnabled(!locked);
            scan.setOnClickListener(v -> scanPassword(true));
            Button qr = secondaryButton(getString(R.string.qr));
            qr.setOnClickListener(v -> showPasswordQr(true));
            actions.addView(change, new LinearLayout.LayoutParams(0, dp(40), 1));
            actions.addView(copy, actionParams());
            actions.addView(scan, actionParams());
            actions.addView(qr, actionParams());
        } else {
            Button paste = secondaryButton(getString(R.string.paste));
            paste.setEnabled(!locked);
            paste.setOnClickListener(v -> pastePassword(false));
            Button scan = secondaryButton(getString(R.string.scan));
            scan.setEnabled(!locked);
            scan.setOnClickListener(v -> scanPassword(false));
            Button qr = secondaryButton(getString(R.string.qr));
            qr.setOnClickListener(v -> showPasswordQr(false));
            actions.addView(paste, new LinearLayout.LayoutParams(0, dp(40), 1));
            actions.addView(scan, actionParams());
            actions.addView(qr, actionParams());
        }
        box.addView(actions, blockParams(dp(6)));
        return box;
    }

    private String passwordFor(boolean sender) {
        return sender ? sendPassword : receivePassword;
    }

    private boolean isPasswordLocked(boolean sender) {
        return sender ? sendSession != null : receiveSession != null;
    }

    private void setPassword(boolean sender, String value) {
        if (sender) {
            sendPassword = value == null ? "" : value;
        } else {
            receivePassword = value == null ? "" : value;
        }
    }

    private boolean isPasswordVisible(boolean sender) {
        return sender ? sendPasswordVisible : receivePasswordVisible;
    }

    private void setPasswordVisible(boolean sender, boolean visible) {
        if (sender) {
            sendPasswordVisible = visible;
        } else {
            receivePasswordVisible = visible;
        }
    }

    private int bumpPasswordVisibilityToken(boolean sender) {
        if (sender) {
            return ++sendPasswordVisibilityToken;
        }
        return ++receivePasswordVisibilityToken;
    }

    private boolean isPasswordVisibilityTokenCurrent(boolean sender, int token) {
        return sender ? sendPasswordVisibilityToken == token : receivePasswordVisibilityToken == token;
    }

    private void applyPasswordVisibility(EditText input, boolean sender) {
        input.setTransformationMethod(isPasswordVisible(sender) ? null : PasswordTransformationMethod.getInstance());
        input.setSelection(input.getText().length());
    }

    private void revealPasswordTemporarily(boolean sender) {
        revealPasswordTemporarily(sender, null);
    }

    private void revealPasswordTemporarily(boolean sender, EditText input) {
        setPasswordVisible(sender, true);
        int token = bumpPasswordVisibilityToken(sender);
        if (input != null) {
            applyPasswordVisibility(input, sender);
        }
        mainHandler.postDelayed(() -> {
            if (!isPasswordVisibilityTokenCurrent(sender, token)) {
                return;
            }
            setPasswordVisible(sender, false);
            if (input != null && input.isAttachedToWindow()) {
                applyPasswordVisibility(input, sender);
            } else {
                render();
            }
        }, 5000);
    }

    private void hidePassword(boolean sender) {
        setPasswordVisible(sender, false);
        bumpPasswordVisibilityToken(sender);
    }

    private View protocolToggle() {
        CheckBox checkBox = new CheckBox(this);
        checkBox.setText(getString(R.string.use_udp_protocol));
        checkBox.setTextColor(Color.rgb(64, 81, 105));
        checkBox.setTextSize(14);
        checkBox.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        checkBox.setChecked(useUdp);
        checkBox.setOnCheckedChangeListener((buttonView, isChecked) -> useUdp = isChecked);
        return checkBox;
    }

    private View fileRow(ShareItem item) {
        LinearLayout row = column();
        row.setPadding(dp(12), dp(9), dp(12), dp(9));
        row.setBackground(rounded(Color.rgb(251, 253, 255), dp(7), Color.rgb(226, 232, 240), 1));

        TextView name = text(item.displayName(), 14, Color.rgb(38, 56, 79), Typeface.BOLD);
        name.setSingleLine(true);
        row.addView(name);

        String detail;
        if (item.isDirectory()) {
            detail = getString(R.string.folder);
        } else {
            String size = item.size() >= 0 ? formatBytes(item.size()) : getString(R.string.unknown_size);
            detail = size + "  " + item.mimeType();
        }
        row.addView(text(detail, 12, muted(), Typeface.NORMAL));
        row.setLayoutParams(blockParams(dp(8)));
        return row;
    }

    private View logPanel() {
        LinearLayout card = card();
        LinearLayout title = row();
        title.setGravity(Gravity.CENTER_VERTICAL);
        title.addView(text(getString(R.string.activity), 16, ink(), Typeface.BOLD), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button toggle = ghostButton(activityExpanded ? getString(R.string.hide) : getString(R.string.show));
        toggle.setOnClickListener(v -> {
            activityExpanded = !activityExpanded;
            render();
        });
        title.addView(toggle, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        Button copy = ghostButton(getString(R.string.copy));
        copy.setOnClickListener(v -> copyLogs());
        title.addView(copy, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        Button clear = ghostButton(getString(R.string.clear));
        clear.setOnClickListener(v -> {
            logs.clear();
            render();
        });
        title.addView(clear, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
        card.addView(title);

        if (!activityExpanded) {
            String summary = activitySummary(currentMetrics());
            card.addView(text(summary, 13, muted(), Typeface.BOLD), blockParams(dp(8)));
            return card;
        }

        card.addView(activityMetrics(), blockParams(dp(8)));

        if (logs.isEmpty()) {
            card.addView(text(getString(R.string.events_will_appear), 13, muted(), Typeface.NORMAL), blockParams(dp(8)));
            return card;
        }

        LinearLayout logBox = column();
        logBox.setPadding(dp(12), dp(10), dp(12), dp(10));
        logBox.setBackground(rounded(Color.rgb(16, 24, 38), dp(8), 0, 0));
        int start = Math.max(0, logs.size() - 80);
        for (int i = start; i < logs.size(); i++) {
            TextView line = text(logs.get(i), 12, Color.rgb(219, 231, 246), Typeface.NORMAL);
            line.setTypeface(Typeface.MONOSPACE);
            line.setTextIsSelectable(true);
            logBox.addView(line);
        }
        card.addView(logBox, blockParams(dp(8)));
        return card;
    }

    private View activityMetrics() {
        TransferMetrics metrics = currentMetrics();
        LinearLayout box = column();
        LinearLayout row1 = row();
        row1.addView(metricBox(getString(R.string.p2p_status), displayStatusLabel(displayStatus(metrics))), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row1.addView(metricBox(getString(R.string.speed), formatRate(currentSpeed(metrics))), metricParams());
        box.addView(row1);

        LinearLayout row2 = row();
        if (sendMode) {
            row2.addView(metricBox(getString(R.string.connections), String.valueOf(metrics.connectedCount)), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
            row2.addView(metricBox(getString(R.string.network), emptyDash(metrics.network)), metricParams());
        } else {
            row2.addView(metricBox(getString(R.string.network), emptyDash(metrics.network)), new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        }
        box.addView(row2, blockParams(dp(8)));
        box.addView(metricBox(getString(R.string.peer), emptyDash(metrics.peer)), blockParams(dp(8)));
        if (!receiveEndpoint.trim().isEmpty()) {
            box.addView(metricBox(getString(R.string.file_endpoint), receiveEndpoint), blockParams(dp(8)));
        }
        return box;
    }

    private TextView sectionTitle(String title) {
        TextView view = text(title, 16, ink(), Typeface.BOLD);
        view.setPadding(0, 0, 0, dp(10));
        return view;
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

    private void startP2PShare() {
        if (shareItems.isEmpty()) {
            Toast.makeText(this, R.string.toast_select_file_first, Toast.LENGTH_SHORT).show();
            return;
        }
        String passphrase = sendPassword.trim();
        if (passphrase.isEmpty()) {
            Toast.makeText(this, R.string.toast_passphrase_required, Toast.LENGTH_SHORT).show();
            return;
        }
        hidePassword(true);
        sendMetrics.reset();
        sendStatus = "Preparing";
        appendLog("info", "Start sharing requested");
        render();
        long runId = ++sendRunId;
        sendSession = bridge.startP2PShare(this, shareItems, passphrase, useUdp, callback(true, runId));
        updateKeepScreenOn();
    }

    private void startP2PReceive() {
        String passphrase = receivePassword.trim();
        if (passphrase.isEmpty()) {
            Toast.makeText(this, R.string.toast_passphrase_required, Toast.LENGTH_SHORT).show();
            return;
        }
        if (saveTreeUri == null) {
            Toast.makeText(this, R.string.toast_choose_save_location, Toast.LENGTH_SHORT).show();
            return;
        }
        hidePassword(false);
        receiveMetrics.reset();
        remoteListStatus = "Waiting for peer";
        downloadStatus = "Idle";
        downloadDoneFiles = 0;
        downloadTotalFiles = 0;
        downloadDoneBytes = 0;
        downloadTotalBytes = 0;
        downloadBytesPerSecond = 0;
        downloadSkippedFiles = 0;
        downloadResumedFiles = 0;
        receiveEndpoint = "";
        remoteCurrentPath = "";
        remoteFiles.clear();
        selectedRemotePaths.clear();
        remoteFileCount = 0;
        remoteDirCount = 0;
        remoteTotalBytes = 0;
        receiveStatus = "Preparing";
        appendLog("info", "Start receiving requested");
        render();
        long runId = ++receiveRunId;
        receiveSession = bridge.startP2PReceive(this, passphrase, useUdp, callback(false, runId));
        updateKeepScreenOn();
    }

    private GoncBridge.EventCallback callback(boolean forSendMode, long runId) {
        return new GoncBridge.EventCallback() {
            @Override
            public void onEvent(String level, String message) {
                mainHandler.post(() -> {
                    if (!isActiveRun(forSendMode, runId)) {
                        return;
                    }
                    updateMetricsFromLog(forSendMode, message);
                    appendLog(level, message);
                    render();
                });
            }

            @Override
            public void onP2PReport(String topic, String status, String network, String mode, String peer, long timestamp, long pid) {
                mainHandler.post(() -> {
                    if (!isActiveRun(forSendMode, runId)) {
                        return;
                    }
                    updateMetricsFromReport(forSendMode, topic, status, network, mode, peer);
                    render();
                });
            }

            @Override
            public void onReady(String endpoint) {
                mainHandler.post(() -> {
                    if (!isActiveRun(forSendMode, runId)) {
                        return;
                    }
                    setStatus(forSendMode, "Ready");
                    updateMetricsReady(forSendMode, endpoint);
                    appendLog("info", "Ready: " + endpoint);
                    if (!forSendMode) {
                        loadRemoteFiles(endpoint, runId);
                    }
                    render();
                });
            }

            @Override
            public void onStopped() {
                mainHandler.post(() -> {
                    if (!isCurrentRun(forSendMode, runId)) {
                        return;
                    }
                    clearSession(forSendMode);
                    setStatus(forSendMode, "Idle");
                    if (!forSendMode) {
                        stopReceiveWorkers();
                    }
                    currentMetrics(forSendMode).markStopped();
                    updateKeepScreenOn();
                    appendLog("warn", "Session stopped");
                    render();
                });
            }

            @Override
            public void onError(Throwable error) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(forSendMode, runId)) {
                        return;
                    }
                    clearSession(forSendMode);
                    setStatus(forSendMode, "Error");
                    if (!forSendMode) {
                        stopReceiveWorkers();
                        remoteListStatus = "Failed";
                    }
                    currentMetrics(forSendMode).p2pStatus = "error";
                    updateKeepScreenOn();
                    appendLog("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    render();
                });
            }
        };
    }

    private void stopSession(boolean forSendMode) {
        GoncBridge.Session current = forSendMode ? sendSession : receiveSession;
        if (current != null) {
            current.stop();
            clearSession(forSendMode);
        }
        if (!forSendMode && receiveDownload != null) {
            receiveDownload.stop();
            receiveDownload = null;
        }
        if (!forSendMode && remoteListSession != null) {
            remoteListSession.stop();
            remoteListSession = null;
        }
        if (!forSendMode) {
            remoteListStatus = "Stopped";
            downloadStatus = "Stopped";
            downloadBytesPerSecond = 0;
            receiveMetrics.inBps = 0;
            receiveMetrics.outBps = 0;
            receiveMetrics.lastTrafficMs = 0;
        }
        setStatus(forSendMode, "Idle");
        currentMetrics(forSendMode).markStopped();
        updateKeepScreenOn();
        appendLog("warn", (forSendMode ? "Send" : "Receive") + " stop requested");
        render();
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
            public void onList(List<HttpReceiver.RemoteFile> files, int fileCount, int dirCount, long totalBytes) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(false, runId)) {
                        return;
                    }
                    remoteListSession = null;
                    if (replaceAll) {
                        remoteFiles.clear();
                        remoteFiles.addAll(files);
                    } else {
                        List<String> refreshedPaths = new ArrayList<>();
                        refreshedPaths.add(targetPath);
                        mergeRemoteFiles(refreshedPaths, files);
                    }
                    recalculateRemoteFileSummary();
                    remoteListStatus = files.isEmpty() ? "No remote files" : (replaceAll ? "Remote list ready" : "Remote list refreshed");
                    updateKeepScreenOn();
                    appendLog("info", "Remote list ready " + displayRemotePath(targetPath) + ": " + fileCount + " file(s), " + dirCount + " folder(s), " + formatBytes(totalBytes));
                    if (!replaceAll) {
                        Toast.makeText(MainActivity.this, R.string.toast_remote_refresh_complete, Toast.LENGTH_SHORT).show();
                    }
                    render();
                });
            }

            @Override
            public void onError(Throwable error) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(false, runId)) {
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
        if (saveTreeUri == null) {
            appendLog("warn", "Save location is not selected");
            Toast.makeText(this, R.string.toast_choose_save_location, Toast.LENGTH_SHORT).show();
            return;
        }
        if (paths == null || paths.isEmpty()) {
            Toast.makeText(this, R.string.toast_select_download_files, Toast.LENGTH_SHORT).show();
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
            public void onList(List<HttpReceiver.RemoteFile> files, int fileCount, int dirCount, long totalBytes) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(false, receiveRunId)) {
                        return;
                    }
                    remoteListSession = null;
                    mergeRemoteFiles(normalizedPaths, files);
                    recalculateRemoteFileSummary();
                    remoteListStatus = files.isEmpty() ? "No remote files" : "Remote list refreshed";
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
                    if (!isCurrentRun(false, receiveRunId)) {
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
        if (saveTreeUri == null) {
            appendLog("warn", "Save location is not selected");
            return;
        }
        if (files == null || files.isEmpty()) {
            Toast.makeText(this, R.string.toast_select_download_files, Toast.LENGTH_SHORT).show();
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
        downloadBytesPerSecond = 0;
        downloadSkippedFiles = 0;
        downloadResumedFiles = 0;
        receiveDownload = HttpReceiver.start(this, endpoint, saveTreeUri, files, resumeDownloads, new HttpReceiver.Callback() {
            @Override
            public void onProgress(int doneFiles, int totalFiles, long doneBytes, long totalBytes, double bytesPerSecond, String current) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(false, runId)) {
                        return;
                    }
                    if (receiveDownload == null) {
                        return;
                    }
                    downloadDoneFiles = doneFiles;
                    downloadTotalFiles = totalFiles;
                    downloadDoneBytes = doneBytes;
                    downloadTotalBytes = totalBytes;
                    downloadBytesPerSecond = bytesPerSecond;
                    receiveMetrics.inBps = bytesPerSecond;
                    receiveMetrics.lastTrafficMs = System.currentTimeMillis();
                    downloadStatus = current == null || current.trim().isEmpty() ? "Preparing download" : "Receiving " + current;
                    render();
                });
            }

            @Override
            public void onComplete(int totalFiles, long totalBytes, int skippedFiles, int resumedFiles) {
                mainHandler.post(() -> {
                    if (!isCurrentRun(false, runId)) {
                        return;
                    }
                    receiveDownload = null;
                    downloadDoneFiles = totalFiles;
                    downloadTotalFiles = totalFiles;
                    downloadDoneBytes = totalBytes;
                    downloadTotalBytes = totalBytes;
                    downloadBytesPerSecond = 0;
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
                    if (!isCurrentRun(false, runId)) {
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

    private void stopReceiveDownload() {
        if (receiveDownload != null) {
            receiveDownload.stop();
            receiveDownload = null;
        }
        downloadStatus = "Stopped";
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

    private void copyPassword(boolean sender) {
        String passphrase = passwordFor(sender).trim();
        if (passphrase.isEmpty()) {
            Toast.makeText(this, R.string.toast_passphrase_empty, Toast.LENGTH_SHORT).show();
            return;
        }
        copyText("Gonc passphrase", passphrase);
        appendLog("info", "Passphrase copied");
        Toast.makeText(this, R.string.toast_passphrase_copied, Toast.LENGTH_SHORT).show();
        render();
    }

    private void pastePassword(boolean sender) {
        if (isPasswordLocked(sender)) {
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
            setPassword(sender, value.toString().trim());
            revealPasswordTemporarily(sender);
            appendLog("info", "Passphrase pasted");
            render();
        }
    }

    private void showPasswordQr(boolean sender) {
        String passphrase = passwordFor(sender).trim();
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
            box.addView(image, blockParams(dp(12)));

            TextView value = text(passphrase, 14, ink(), Typeface.BOLD);
            value.setTypeface(Typeface.MONOSPACE, Typeface.BOLD);
            value.setTextIsSelectable(true);
            value.setGravity(Gravity.CENTER);
            value.setSingleLine(false);
            value.setPadding(dp(10), dp(8), dp(10), dp(8));
            value.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));
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

    private void showSourceDialog() {
        Dialog dialog = new Dialog(this);
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE);
        LinearLayout box = column();
        box.setPadding(dp(18), dp(18), dp(18), dp(18));
        box.setBackground(rounded(Color.WHITE, dp(8), 0, 0));

        TextView title = text(getString(R.string.source_title), 16, ink(), Typeface.BOLD);
        title.setGravity(Gravity.CENTER);
        box.addView(title);

        TextView desc = text(getString(R.string.source_description), 13, muted(), Typeface.NORMAL);
        desc.setSingleLine(false);
        box.addView(desc, blockParams(dp(10)));

        TextView url = text(SOURCE_URL, 13, Color.rgb(32, 101, 165), Typeface.BOLD);
        url.setTextIsSelectable(true);
        url.setSingleLine(false);
        url.setPadding(dp(10), dp(8), dp(10), dp(8));
        url.setBackground(rounded(Color.rgb(248, 251, 255), dp(7), Color.rgb(226, 232, 240), 1));
        box.addView(url, blockParams(dp(8)));

        LinearLayout actions = row();
        Button open = secondaryButton(getString(R.string.open));
        open.setOnClickListener(v -> {
            try {
                startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(SOURCE_URL)));
            } catch (RuntimeException error) {
                copySourceUrl();
            }
        });
        Button copy = secondaryButton(getString(R.string.copy));
        copy.setOnClickListener(v -> copySourceUrl());
        Button close = secondaryButton(getString(R.string.close));
        close.setOnClickListener(v -> dialog.dismiss());
        actions.addView(open, new LinearLayout.LayoutParams(0, dp(42), 1));
        actions.addView(copy, actionParams(dp(42)));
        actions.addView(close, actionParams(dp(42)));
        box.addView(actions, blockParams(dp(12)));

        dialog.setContentView(box);
        dialog.show();
    }

    private void copySourceUrl() {
        copyText(getString(R.string.source_title), SOURCE_URL);
        Toast.makeText(this, R.string.toast_source_copied, Toast.LENGTH_SHORT).show();
    }

    private void scanPassword(boolean sender) {
        if (isPasswordLocked(sender)) {
            return;
        }
        scanningForSendMode = sender;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M && checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.CAMERA}, REQUEST_CAMERA_PERMISSION);
            return;
        }
        launchQrScanner();
    }

    private void launchQrScanner() {
        Intent intent = new Intent(this, QrScanActivity.class);
        intent.setAction(Intents.Scan.ACTION);
        intent.putExtra(Intents.Scan.MODE, Intents.Scan.QR_CODE_MODE);
        intent.putExtra(Intents.Scan.PROMPT_MESSAGE, getString(R.string.scan_prompt));
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

    private void copyText(String label, String value) {
        ClipboardManager clipboard = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (clipboard != null) {
            clipboard.setPrimaryClip(ClipData.newPlainText(label, value));
        }
    }

    private String currentStatus() {
        return sendMode ? sendStatus : receiveStatus;
    }

    private String statusSummary() {
        TransferMetrics metrics = currentMetrics();
        String state = displayStatus(metrics);
        if ("idle".equals(state)) {
            state = currentStatus();
        }
        state = displayStatusLabel(state);
        if (!sendMode) {
            return state + "  " + formatRate(currentSpeed(metrics));
        }
        return state + "  " + metrics.connectedCount + "  " + formatRate(currentSpeed(metrics));
    }

    private String activitySummary(TransferMetrics metrics) {
        String state = displayStatus(metrics);
        if ("idle".equals(state)) {
            state = currentStatus();
        }
        state = displayStatusLabel(state);
        if (!sendMode) {
            return state + "  " + formatRate(currentSpeed(metrics));
        }
        return state + "  " + metrics.connectedCount + " conn  " + formatRate(currentSpeed(metrics));
    }

    private void setStatus(boolean forSendMode, String nextStatus) {
        if (forSendMode) {
            sendStatus = nextStatus;
        } else {
            receiveStatus = nextStatus;
        }
    }

    private void clearSession(boolean forSendMode) {
        if (forSendMode) {
            sendSession = null;
        } else {
            receiveSession = null;
        }
    }

    private boolean isCurrentRun(boolean forSendMode, long runId) {
        return forSendMode ? sendRunId == runId : receiveRunId == runId;
    }

    private boolean isActiveRun(boolean forSendMode, long runId) {
        if (!isCurrentRun(forSendMode, runId)) {
            return false;
        }
        return forSendMode ? sendSession != null : receiveSession != null;
    }

    private boolean isAnySessionRunning() {
        return sendSession != null || receiveSession != null || remoteListSession != null || receiveDownload != null;
    }

    private TransferMetrics currentMetrics() {
        return currentMetrics(sendMode);
    }

    private TransferMetrics currentMetrics(boolean forSendMode) {
        return forSendMode ? sendMetrics : receiveMetrics;
    }

    private void updateMetricsReady(boolean forSendMode, String endpoint) {
        TransferMetrics metrics = currentMetrics(forSendMode);
        if (!metrics.p2pStatus.equals("connected")) {
            metrics.p2pStatus = "ready";
        }
    }

    private void updateMetricsFromReport(boolean forSendMode, String topic, String status, String network, String mode, String peer) {
        TransferMetrics metrics = currentMetrics(forSendMode);
        if (status != null && !status.trim().isEmpty()) {
            metrics.p2pStatus = status.trim();
        }
        if (network != null && !network.trim().isEmpty()) {
            metrics.network = network.trim();
        }
        if (peer != null && !peer.trim().isEmpty()) {
            metrics.peer = peer.trim();
        }
        String key = topic != null && !topic.trim().isEmpty() ? topic.trim() : emptyDash(peer);
        if ("-".equals(key)) {
            key = mode != null && !mode.trim().isEmpty() ? mode.trim() : "default";
        }
        metrics.sessions.put(key, normalizeMetricStatus(status));
        metrics.recountConnections();
    }

    private String normalizeMetricStatus(String status) {
        if (status == null || status.trim().isEmpty()) {
            return "-";
        }
        return status.trim().toLowerCase(Locale.ROOT);
    }

    private void updateMetricsFromLog(boolean forSendMode, String message) {
        TransferMetrics metrics = currentMetrics(forSendMode);
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
            metrics.p2pStatus = "connected";
            if (compact.contains("P2P(TCP)")) {
                metrics.network = "TCP";
            } else if (compact.contains("P2P(UDP)")) {
                metrics.network = "UDP";
            }
            metrics.connectedCount = Math.max(metrics.connectedCount, 1);
        } else if (compact.contains("P2P failed") || compact.contains("hole punching failed")) {
            metrics.p2pStatus = "failed";
        } else if (compact.contains("P2P stopped") || compact.contains("disconnected")) {
            metrics.markStopped();
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

    private void updateKeepScreenOn() {
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
        Map<String, ShareItem> existing = new LinkedHashMap<>();
        for (ShareItem item : shareItems) {
            existing.put(item.uri().toString(), item);
        }
        for (Uri uri : uris) {
            takeReadPermission(uri, sourceIntent);
            existing.put(uri.toString(), loadShareItem(uri));
        }
        shareItems.clear();
        shareItems.addAll(existing.values());
    }

    private void addTreeUri(Uri uri, Intent sourceIntent) {
        takeReadPermission(uri, sourceIntent);
        Map<String, ShareItem> existing = new LinkedHashMap<>();
        for (ShareItem item : shareItems) {
            existing.put(item.uri().toString(), item);
        }
        existing.put(uri.toString(), loadTreeShareItem(uri));
        shareItems.clear();
        shareItems.addAll(existing.values());
        appendLog("info", "Shared folder added: " + shareItems.get(shareItems.size() - 1).displayName());
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
        if (TrafficLogParser.isProgressLog(message)) {
            return;
        }
        logs.add(level.toUpperCase(Locale.ROOT) + "  " + message);
        while (logs.size() > 1000) {
            logs.remove(0);
        }
    }

    private Button modeButton(String label, boolean active) {
        Button button = new Button(this);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(14);
        button.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        button.setTextColor(active ? Color.WHITE : Color.rgb(82, 101, 125));
        button.setBackground(rounded(active ? ink() : Color.TRANSPARENT, dp(6), 0, 0));
        return button;
    }

    private Button primaryButton(String label) {
        Button button = button(label, Color.WHITE, Color.rgb(40, 112, 216));
        button.setTextSize(16);
        return button;
    }

    private Button dangerButton(String label) {
        Button button = button(label, Color.WHITE, Color.rgb(201, 63, 63));
        button.setTextSize(16);
        return button;
    }

    private Button secondaryButton(String label) {
        return button(label, ink(), Color.rgb(237, 242, 247));
    }

    private Button ghostButton(String label) {
        return button(label, Color.rgb(40, 112, 216), Color.TRANSPARENT);
    }

    private Button button(String label, int color, int background) {
        Button button = new Button(this);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(14);
        button.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        button.setTextColor(color);
        button.setBackground(rounded(background, dp(6), 0, 0));
        return button;
    }

    private TextView text(String value, int sp, int color, int style) {
        TextView view = new TextView(this);
        view.setText(value);
        view.setTextSize(sp);
        view.setTextColor(color);
        view.setTypeface(Typeface.DEFAULT, style);
        return view;
    }

    private LinearLayout card() {
        LinearLayout card = column();
        card.setPadding(dp(16), dp(16), dp(16), dp(16));
        card.setBackground(rounded(Color.WHITE, dp(8), Color.rgb(216, 226, 238), 1));
        card.setLayoutParams(blockParams(dp(12)));
        return card;
    }

    private LinearLayout row() {
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.HORIZONTAL);
        return layout;
    }

    private LinearLayout column() {
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        return layout;
    }

    private LinearLayout.LayoutParams blockParams() {
        return blockParams(dp(10));
    }

    private LinearLayout.LayoutParams blockParams(int topMargin) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
        );
        params.setMargins(0, topMargin, 0, 0);
        return params;
    }

    private LinearLayout.LayoutParams actionParams() {
        return actionParams(dp(40));
    }

    private LinearLayout.LayoutParams actionParams(int height) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, height, 1);
        params.setMargins(dp(8), 0, 0, 0);
        return params;
    }

    private LinearLayout.LayoutParams metricParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        params.setMargins(dp(8), 0, 0, 0);
        return params;
    }

    private GradientDrawable rounded(int color, int radius, int strokeColor, int strokeWidthDp) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(color);
        drawable.setCornerRadius(radius);
        if (strokeWidthDp > 0) {
            drawable.setStroke(dp(strokeWidthDp), strokeColor);
        }
        return drawable;
    }

    private int dp(int value) {
        return (int) (value * getResources().getDisplayMetrics().density + 0.5f);
    }

    private int ink() {
        return Color.rgb(23, 32, 51);
    }

    private int muted() {
        return Color.rgb(100, 116, 139);
    }

    private String formatBytes(long value) {
        if (value < 0) {
            return getString(R.string.unknown_size);
        }
        String[] units = {"B", "KB", "MB", "GB", "TB"};
        double size = value;
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            size /= 1024;
            unit++;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    private String formatRate(double bytesPerSecond) {
        if (bytesPerSecond <= 0) {
            return "0 B/s";
        }
        String[] units = {"B/s", "KB/s", "MB/s", "GB/s"};
        double size = bytesPerSecond;
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            size /= 1024;
            unit++;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    private String formatPercent(long done, long total) {
        if (total <= 0) {
            return "0%";
        }
        double value = Math.min(100, Math.max(0, done * 100.0 / total));
        return String.format(Locale.ROOT, "%.1f%%", value);
    }

    private String emptyDash(String value) {
        return value == null || value.trim().isEmpty() ? "-" : value.trim();
    }

    private String titleCase(String value) {
        if (value == null || value.trim().isEmpty()) {
            return "-";
        }
        String clean = value.trim();
        return clean.substring(0, 1).toUpperCase(Locale.ROOT) + clean.substring(1);
    }

    private static final class TransferMetrics {
        final Map<String, String> sessions = new LinkedHashMap<>();
        String p2pStatus = "idle";
        String network = "-";
        String peer = "-";
        int connectedCount;
        int connectingCount;
        double inBps;
        double outBps;
        long lastTrafficMs;

        void reset() {
            sessions.clear();
            p2pStatus = "starting";
            network = "-";
            peer = "-";
            connectedCount = 0;
            connectingCount = 0;
            inBps = 0;
            outBps = 0;
            lastTrafficMs = 0;
        }

        void markStopped() {
            p2pStatus = "idle";
            connectedCount = 0;
            connectingCount = 0;
            inBps = 0;
            outBps = 0;
            lastTrafficMs = 0;
            sessions.clear();
        }

        void recountConnections() {
            connectedCount = 0;
            connectingCount = 0;
            for (String status : sessions.values()) {
                if ("connected".equals(status)) {
                    connectedCount++;
                } else if ("connecting".equals(status)) {
                    connectingCount++;
                }
            }
        }
    }
}
