package cn.threatexpert.gonc;

import android.app.AlertDialog;
import android.graphics.Color;
import android.graphics.Typeface;
import android.text.Editable;
import android.text.InputType;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.TextView;

import java.util.ArrayList;
import java.io.File;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Send ("share files") module: owns the chosen items, passphrase/protocol
 * state, the share view and the gonc share session. The host picks files
 * (content-resolver/permission work) and feeds {@link ShareItem}s in via
 * {@link #addFiles}/{@link #addFolder}.
 */
final class SendController {
    private ModuleHost host;
    private final List<ShareItem> shareItems = new ArrayList<>();
    private final TransferMetrics metrics = new TransferMetrics();

    private boolean useUdp;
    private boolean passwordVisible;
    private boolean sendQrHasConnected;
    private int passwordVisibilityToken;
    private String password = Passwords.generate();
    private String status = "Idle";
    private GoncBridge.Session session;
    private long runId;

    SendController(ModuleHost host) {
        this.host = host;
    }

    /** Rebind to the current host after an Activity recreation (config change). */
    void attach(ModuleHost host) {
        this.host = host;
    }

    boolean isRunning() {
        return session != null;
    }

    /** Foreground-service contribution: null when idle; dot is green once a peer is connected. */
    GoncForegroundService.State foregroundState() {
        if (session == null) {
            return null;
        }
        GoncForegroundService.Dot dot = metrics.connectedCount > 0
                ? GoncForegroundService.Dot.GREEN
                : GoncForegroundService.Dot.YELLOW;
        return new GoncForegroundService.State(dot, -1);
    }

    TransferMetrics metrics() {
        return metrics;
    }

    String status() {
        return status;
    }

    // --- items ------------------------------------------------------------

    boolean addFiles(List<ShareItem> newItems) {
        Set<String> existing = new HashSet<>();
        for (ShareItem item : shareItems) {
            existing.add(item.uri().toString());
        }
        boolean added = false;
        for (ShareItem item : newItems) {
            if (existing.add(item.uri().toString())) {
                shareItems.add(item);
                added = true;
            }
        }
        if (added) {
            syncSource();
            host.requestRender();
        }
        return added;
    }

    void addFolder(ShareItem item) {
        if (addFiles(java.util.Collections.singletonList(item))) {
            host.log("info", "Shared folder added: " + item.displayName());
        }
    }

    private void removeItem(ShareItem item) {
        if (!shareItems.remove(item)) {
            return;
        }
        syncSource();
        deleteOwned(item);
        host.log("info", "Removed shared item: " + item.displayName());
        host.requestRender();
    }

    private void clearItems() {
        if (shareItems.isEmpty()) {
            return;
        }
        List<ShareItem> removed = new ArrayList<>(shareItems);
        shareItems.clear();
        syncSource();
        for (ShareItem item : removed) {
            deleteOwned(item);
        }
        host.log("info", "Cleared shared items");
        host.requestRender();
    }

    private void deleteOwned(ShareItem item) {
        if (item.ownedFile() != null) {
            GeneratedSendFiles.deleteOwned(generatedSendRoot(), item.ownedFile());
        }
    }

    private File generatedSendRoot() {
        return new File(host.context().getCacheDir(), "generated-send");
    }

    private void syncSource() {
        if (session != null) {
            session.updateShareItems(shareItems);
        }
    }

    // --- view -------------------------------------------------------------

    View panel() {
        UiKit u = host.ui();
        LinearLayout card = u.card();

        card.addView(contentHeader(), u.blockParams(0));
        if (shareItems.isEmpty()) {
            TextView empty = u.text(string(R.string.send_empty_add_hint), 14, u.muted(), Typeface.BOLD);
            empty.setGravity(Gravity.CENTER);
            empty.setPadding(u.dp(12), u.dp(22), u.dp(12), u.dp(22));
            empty.setBackground(u.rounded(Color.rgb(248, 251, 255), u.dp(8), Color.rgb(143, 168, 195), 1));
            empty.setOnClickListener(v -> showAddTypeDialog());
            card.addView(empty, u.blockParams());
        } else {
            for (ShareItem item : shareItems) {
                card.addView(fileRow(item));
            }
            TextView continueAdd = u.text(string(R.string.send_continue_add), 13,
                    Color.rgb(40, 112, 216), Typeface.BOLD);
            continueAdd.setGravity(Gravity.CENTER);
            continueAdd.setPadding(u.dp(8), u.dp(12), u.dp(8), u.dp(6));
            continueAdd.setOnClickListener(v -> showAddTypeDialog());
            card.addView(continueAdd, u.blockParams(0));
        }

        card.addView(u.sectionBoundaryTitle(string(R.string.passphrase_config), true), u.blockParams(u.dp(14)));
        card.addView(passwordField());
        card.addView(u.sectionDivider(), u.dividerParams(u.dp(12)));
        card.addView(protocolToggle());

        Button primary = session == null
                ? u.primaryButton(string(R.string.start_sharing))
                : u.dangerButton(string(R.string.stop_sharing));
        primary.setOnClickListener(v -> {
            if (session == null) {
                start();
            } else {
                stop();
            }
        });
        card.addView(primary, u.blockParams(u.dp(12)));
        return card;
    }

    private View contentHeader() {
        UiKit u = host.ui();
        LinearLayout header = u.row();
        header.setGravity(Gravity.CENTER_VERTICAL);
        String title = shareItems.isEmpty()
                ? string(R.string.send_content_title)
                : host.context().getString(R.string.send_content_title_count, shareItems.size());
        header.addView(u.text(title, 13, Color.rgb(64, 81, 105), Typeface.BOLD),
                new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        if (!shareItems.isEmpty()) {
            Button clear = u.compactGhostButton(string(R.string.clear));
            clear.setTextColor(Color.rgb(138, 46, 46));
            clear.setOnClickListener(v -> clearItems());
            header.addView(clear, new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(32)));
        }
        return header;
    }

    private View fileRow(ShareItem item) {
        UiKit u = host.ui();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(u.dp(12), u.dp(9), u.dp(12), u.dp(9));
        row.setBackground(u.rounded(Color.rgb(251, 253, 255), u.dp(7), Color.rgb(226, 232, 240), 1));

        ImageView icon = new ImageView(host.context());
        icon.setImageResource(iconFor(item));
        icon.setScaleType(ImageView.ScaleType.CENTER_INSIDE);
        row.addView(icon, new LinearLayout.LayoutParams(u.dp(48), u.dp(48)));

        LinearLayout labels = u.column();
        labels.setPadding(u.dp(10), 0, u.dp(8), 0);
        TextView name = u.text(item.displayName(), 14, Color.rgb(38, 56, 79), Typeface.BOLD);
        name.setSingleLine(true);
        labels.addView(name);

        String detail;
        if (item.isDirectory()) {
            detail = string(R.string.folder);
        } else {
            String size = item.size() >= 0 ? u.formatBytes(item.size()) : string(R.string.unknown_size);
            detail = size + "  " + item.mimeType();
        }
        labels.addView(u.text(detail, 12, u.muted(), Typeface.NORMAL));
        row.addView(labels, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

        Button remove = u.compactGhostButton("");
        remove.setText("×");
        remove.setTextSize(20);
        remove.setTextColor(Color.rgb(138, 46, 46));
        remove.setContentDescription(string(R.string.remove) + " " + item.displayName());
        remove.setBackground(u.rounded(Color.rgb(241, 245, 249), u.dp(16), 0, 0));
        remove.setOnClickListener(v -> removeItem(item));
        row.addView(remove, new LinearLayout.LayoutParams(u.dp(32), u.dp(32)));
        row.setLayoutParams(u.blockParams(u.dp(8)));
        return row;
    }

    private int iconFor(ShareItem item) {
        if (item.isDirectory()) {
            return R.drawable.ic_send_folder;
        }
        String mimeType = item.mimeType();
        if (mimeType.startsWith("image/") || mimeType.startsWith("video/")) {
            return R.drawable.ic_send_media;
        }
        if (mimeType.startsWith("text/")) {
            return R.drawable.ic_send_text;
        }
        return R.drawable.ic_send_file;
    }

    private void showAddTypeDialog() {
        UiKit u = host.ui();
        LinearLayout choices = u.column();
        choices.setPadding(u.dp(8), u.dp(4), u.dp(8), u.dp(4));
        AlertDialog dialog = new AlertDialog.Builder(host.context())
                .setTitle(R.string.send_add_type_title)
                .setView(choices)
                .setNegativeButton(R.string.cancel, null)
                .create();
        addChoice(choices, R.drawable.ic_send_file, R.string.add_files, () -> {
            dialog.dismiss();
            host.pickSendFiles();
        });
        addChoice(choices, R.drawable.ic_send_folder, R.string.add_folder, () -> {
            dialog.dismiss();
            host.pickSendFolder();
        });
        addChoice(choices, R.drawable.ic_send_media, R.string.add_media, () -> {
            dialog.dismiss();
            host.pickSendMedia();
        });
        addChoice(choices, R.drawable.ic_send_text, R.string.add_text, () -> {
            dialog.dismiss();
            showAuthoredTextDialog();
        });
        addChoice(choices, R.drawable.ic_send_clipboard, R.string.add_clipboard, () -> {
            dialog.dismiss();
            host.importSendClipboard();
        });
        dialog.show();
    }

    private void showAuthoredTextDialog() {
        UiKit u = host.ui();
        EditText input = new EditText(host.context());
        input.setMinLines(5);
        input.setGravity(Gravity.TOP | Gravity.START);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE);
        input.setPadding(u.dp(12), u.dp(10), u.dp(12), u.dp(10));
        input.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        AlertDialog dialog = new AlertDialog.Builder(host.context())
                .setTitle(R.string.add_text_title)
                .setView(input)
                .setNegativeButton(R.string.cancel, null)
                .setPositiveButton(R.string.add_content_action, null)
                .create();
        dialog.setOnShowListener(ignored -> dialog.getButton(AlertDialog.BUTTON_POSITIVE)
                .setOnClickListener(v -> {
                    String value = input.getText().toString();
                    if (value.isEmpty()) {
                        return;
                    }
                    host.addAuthoredSendText(value);
                    dialog.dismiss();
                }));
        dialog.show();
    }

    private void addChoice(LinearLayout parent, int iconRes, int labelRes, Runnable action) {
        UiKit u = host.ui();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(u.dp(10), u.dp(8), u.dp(10), u.dp(8));
        ImageView icon = new ImageView(host.context());
        icon.setImageResource(iconRes);
        row.addView(icon, new LinearLayout.LayoutParams(u.dp(28), u.dp(28)));
        TextView label = u.text(string(labelRes), 15, u.ink(), Typeface.BOLD);
        label.setPadding(u.dp(12), 0, 0, 0);
        row.addView(label, new LinearLayout.LayoutParams(
                0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        row.setMinimumHeight(u.dp(48));
        row.setOnClickListener(v -> action.run());
        parent.addView(row, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
    }

    private View passwordField() {
        UiKit u = host.ui();
        if (session != null) {
            LinearLayout qrOnly = u.column();
            View qr = PassphraseQrView.create(
                    host.context(), u, password, TransferInlineQrState.inlineQrSizeDp(),
                    sendQrHasConnected,
                    () -> host.showPassphraseQr(password.trim()),
                    () -> host.toast(R.string.inline_qr_generation_failed));
            int qrSize = u.dp(TransferInlineQrState.inlineQrSizeDp());
            LinearLayout.LayoutParams qrParams = new LinearLayout.LayoutParams(qrSize, qrSize);
            qrParams.gravity = Gravity.CENTER_HORIZONTAL;
            qrOnly.addView(qr, qrParams);
            return qrOnly;
        }
        boolean locked = session != null;
        LinearLayout box = u.column();
        box.addView(u.text(string(R.string.passphrase_hint), 12, u.muted(), Typeface.NORMAL), u.blockParams(u.dp(4)));

        LinearLayout line = u.row();
        EditText input = new EditText(host.context());
        input.setSingleLine(true);
        input.setText(password);
        input.setTextColor(u.ink());
        input.setTextSize(15);
        input.setHint(string(R.string.passphrase_input_hint));
        input.setHintTextColor(Color.rgb(148, 163, 184));
        input.setPadding(u.dp(12), 0, u.dp(12), 0);
        input.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        input.setEnabled(!locked);
        applyPasswordVisibility(input);
        input.addTextChangedListener(new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
                if (session != null) {
                    return;
                }
                password = s.toString();
                revealPasswordTemporarily(input);
            }

            @Override
            public void afterTextChanged(Editable s) {
            }
        });
        line.addView(input, new LinearLayout.LayoutParams(0, u.dp(46), 1));
        box.addView(line, u.blockParams(u.dp(8)));

        LinearLayout actions = u.row();
        Button change = u.secondaryButton(string(R.string.random_passphrase));
        u.setControlEnabled(change, !locked);
        change.setOnClickListener(v -> randomizePassword());
        Button copy = u.secondaryButton(string(R.string.copy));
        copy.setOnClickListener(v -> copyPassword());
        Button scan = u.secondaryButton(string(R.string.scan));
        u.setControlEnabled(scan, !locked);
        scan.setOnClickListener(v -> scanPassword());
        Button qr = u.secondaryButton(string(R.string.qr));
        qr.setOnClickListener(v -> host.showPassphraseQr(password.trim()));
        actions.addView(change, new LinearLayout.LayoutParams(0, u.dp(40), 1));
        actions.addView(copy, u.actionParams());
        actions.addView(scan, u.actionParams());
        actions.addView(qr, u.actionParams());
        box.addView(actions, u.blockParams(u.dp(6)));
        return box;
    }

    private View protocolToggle() {
        UiKit u = host.ui();
        LinearLayout box = u.column();
        CheckBox checkBox = new CheckBox(host.context());
        checkBox.setText(string(R.string.use_udp_protocol));
        checkBox.setTextColor(Color.rgb(64, 81, 105));
        checkBox.setTextSize(14);
        checkBox.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        checkBox.setChecked(useUdp);
        u.setControlEnabled(checkBox, session == null);
        checkBox.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (session != null) {
                return;
            }
            useUdp = isChecked;
        });
        box.addView(checkBox);
        TextView hint = u.text(string(R.string.use_udp_protocol_hint), 12, u.muted(), Typeface.NORMAL);
        hint.setPadding(u.dp(4), 0, 0, 0);
        box.addView(hint);
        return box;
    }

    // --- passphrase actions ----------------------------------------------

    private void applyPasswordVisibility(EditText input) {
        input.setTransformationMethod(passwordVisible ? null : PasswordTransformationMethod.getInstance());
        input.setSelection(input.getText().length());
    }

    private void revealPasswordTemporarily() {
        revealPasswordTemporarily(null);
    }

    private void revealPasswordTemporarily(EditText input) {
        passwordVisible = true;
        int token = ++passwordVisibilityToken;
        if (input != null) {
            applyPasswordVisibility(input);
        }
        host.mainHandler().postDelayed(() -> {
            if (passwordVisibilityToken != token) {
                return;
            }
            passwordVisible = false;
            if (input != null && input.isAttachedToWindow()) {
                applyPasswordVisibility(input);
            } else {
                host.requestRender();
            }
        }, 5000);
    }

    private void hidePassword() {
        passwordVisible = false;
        passwordVisibilityToken++;
    }

    private void randomizePassword() {
        if (session != null) {
            return;
        }
        password = Passwords.generate();
        revealPasswordTemporarily();
        host.log("info", "Passphrase randomized");
        host.requestRender();
    }

    private void copyPassword() {
        String passphrase = password.trim();
        if (passphrase.isEmpty()) {
            host.toast(R.string.toast_passphrase_empty);
            return;
        }
        host.copyText("Gonc passphrase", passphrase);
        host.log("info", "Passphrase copied");
        host.toast(R.string.toast_passphrase_copied);
        host.requestRender();
    }

    private void scanPassword() {
        if (session != null) {
            return;
        }
        host.scanPassphrase(result -> {
            if (session != null) {
                return;
            }
            password = result.trim();
            revealPasswordTemporarily();
            host.log("info", "Passphrase scanned");
            host.requestRender();
        });
    }

    // --- session ----------------------------------------------------------

    private void start() {
        if (shareItems.isEmpty()) {
            host.toast(R.string.toast_select_file_first);
            return;
        }
        String passphrase = password.trim();
        if (passphrase.isEmpty()) {
            host.toast(R.string.toast_passphrase_required);
            return;
        }
        if (Passwords.isWeak(passphrase)) {
            host.toast(R.string.toast_passphrase_weak);
            return;
        }
        hidePassword();
        PassphraseQrView.clearCache();
        metrics.reset();
        sendQrHasConnected = TransferInlineQrState.newSendRunLatch();
        status = "Preparing";
        host.log("info", "Start sharing requested");
        long id = ++runId;
        session = host.bridge().startP2PShare(host.context(), shareItems, passphrase, useUdp, callback(id));
        host.refreshForegroundService();
        host.requestRender();
    }

    private void stop() {
        GoncBridge.Session current = session;
        if (current != null) {
            current.stop();
            session = null;
        }
        status = "Idle";
        metrics.markStopped();
        sendQrHasConnected = TransferInlineQrState.newSendRunLatch();
        PassphraseQrView.clearCache();
        host.refreshForegroundService();
        host.log("warn", "Send stop requested");
        host.requestRender();
    }

    void shutdown() {
        GoncBridge.Session current = session;
        session = null;
        sendQrHasConnected = TransferInlineQrState.newSendRunLatch();
        PassphraseQrView.clearCache();
        if (current != null) {
            current.stop();
        }
    }

    void endTask() {
        runId++;
        shutdown();
    }

    void resetForFreshLaunch() {
        for (ShareItem item : new ArrayList<>(shareItems)) {
            deleteOwned(item);
        }
        shareItems.clear();
        useUdp = false;
        passwordVisible = false;
        password = Passwords.generate();
        status = "Idle";
        passwordVisibilityToken++;
        metrics.reset();
        sendQrHasConnected = TransferInlineQrState.newSendRunLatch();
        PassphraseQrView.clearCache();
    }

    private GoncBridge.EventCallback callback(long id) {
        return new GoncBridge.EventCallback() {
            @Override
            public void onEvent(String level, String message) {
                host.mainHandler().post(() -> {
                    if (runId != id || session == null) {
                        return;
                    }
                    host.updateMetricsFromLog(metrics, message);
                    host.log(level, message);
                    host.requestBackgroundRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onP2PReport(String topic, String side, String reportStatus, String network, String mode, String peer, long timestamp, long pid) {
                host.mainHandler().post(() -> {
                    if (runId != id || session == null) {
                        return;
                    }
                    host.updateMetricsFromReport(metrics, topic, reportStatus, network, mode, peer);
                    boolean wasConnected = sendQrHasConnected;
                    sendQrHasConnected = TransferInlineQrState.latchSendConnected(
                            sendQrHasConnected, metrics.connectedCount);
                    if (!wasConnected && sendQrHasConnected
                            && TransferInlineQrState.shouldClearQrCache(runId, id)) {
                        PassphraseQrView.clearCache();
                    }
                    host.requestRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onTraffic(String side, long inBytes, long outBytes, double inBps, double outBps, long elapsed, long connCount, boolean isFinal) {
                host.mainHandler().post(() -> {
                    if (runId != id || session == null) {
                        return;
                    }
                    host.updateMetricsFromTraffic(metrics, inBytes, outBytes, inBps, outBps);
                    host.requestBackgroundRender();
                    host.refreshForegroundService();
                });
            }

            @Override
            public void onReady(String endpoint) {
                host.mainHandler().post(() -> {
                    if (runId != id || session == null) {
                        return;
                    }
                    status = "Ready";
                    metrics.p2pStatus = "connected";
                    host.log("info", "Ready: " + endpoint);
                    host.requestRender();
                });
            }

            @Override
            public void onStopped() {
                host.mainHandler().post(() -> {
                    if (runId != id) {
                        return;
                    }
                    session = null;
                    sendQrHasConnected = TransferInlineQrState.newSendRunLatch();
                    if (TransferInlineQrState.shouldClearQrCache(runId, id)) {
                        PassphraseQrView.clearCache();
                    }
                    status = "Idle";
                    metrics.markStopped();
                    host.refreshForegroundService();
                    host.log("warn", "Session stopped");
                    host.requestRender();
                });
            }

            @Override
            public void onError(Throwable error) {
                host.mainHandler().post(() -> {
                    if (runId != id) {
                        return;
                    }
                    session = null;
                    sendQrHasConnected = TransferInlineQrState.newSendRunLatch();
                    if (TransferInlineQrState.shouldClearQrCache(runId, id)) {
                        PassphraseQrView.clearCache();
                    }
                    status = "Error";
                    metrics.p2pStatus = "error";
                    host.refreshForegroundService();
                    host.log("error", error.getMessage() == null ? error.toString() : error.getMessage());
                    host.requestRender();
                });
            }
        };
    }

    private String string(int resId) {
        return host.context().getString(resId);
    }
}
