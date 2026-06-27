package cn.threatexpert.gonc;

import android.graphics.Color;
import android.graphics.Typeface;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

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

    void addFiles(List<ShareItem> newItems) {
        Map<String, ShareItem> existing = new LinkedHashMap<>();
        for (ShareItem item : shareItems) {
            existing.put(item.uri().toString(), item);
        }
        for (ShareItem item : newItems) {
            existing.put(item.uri().toString(), item);
        }
        shareItems.clear();
        shareItems.addAll(existing.values());
        syncSource();
        host.requestRender();
    }

    void addFolder(ShareItem item) {
        Map<String, ShareItem> existing = new LinkedHashMap<>();
        for (ShareItem it : shareItems) {
            existing.put(it.uri().toString(), it);
        }
        existing.put(item.uri().toString(), item);
        shareItems.clear();
        shareItems.addAll(existing.values());
        syncSource();
        host.log("info", "Shared folder added: " + shareItems.get(shareItems.size() - 1).displayName());
        host.requestRender();
    }

    private void removeItem(ShareItem item) {
        shareItems.remove(item);
        syncSource();
        host.log("info", "Removed shared item: " + item.displayName());
        host.requestRender();
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

        card.addView(u.sectionBoundaryTitle(string(R.string.share_files_config), false), u.blockParams(0));
        if (shareItems.isEmpty()) {
            TextView empty = u.text(string(R.string.no_files_selected), 14, u.muted(), Typeface.NORMAL);
            empty.setGravity(Gravity.CENTER);
            empty.setPadding(u.dp(12), u.dp(22), u.dp(12), u.dp(22));
            empty.setBackground(u.rounded(Color.rgb(248, 251, 255), u.dp(8), Color.rgb(143, 168, 195), 1));
            card.addView(empty, u.blockParams());
        } else {
            for (ShareItem item : shareItems) {
                card.addView(fileRow(item));
            }
        }

        LinearLayout actions = u.row();
        Button add = u.secondaryButton(string(R.string.add_files));
        add.setOnClickListener(v -> host.pickSendFiles());
        Button addFolder = u.secondaryButton(string(R.string.add_folder));
        addFolder.setOnClickListener(v -> host.pickSendFolder());
        actions.addView(add, new LinearLayout.LayoutParams(0, u.dp(42), 1));
        actions.addView(addFolder, new LinearLayout.LayoutParams(0, u.dp(42), 1));
        card.addView(actions, u.blockParams());

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

    private View fileRow(ShareItem item) {
        UiKit u = host.ui();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(u.dp(12), u.dp(9), u.dp(12), u.dp(9));
        row.setBackground(u.rounded(Color.rgb(251, 253, 255), u.dp(7), Color.rgb(226, 232, 240), 1));

        LinearLayout labels = u.column();
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

        Button remove = u.ghostButton(string(R.string.remove));
        remove.setOnClickListener(v -> removeItem(item));
        row.addView(remove, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(38)));
        row.setLayoutParams(u.blockParams(u.dp(8)));
        return row;
    }

    private View passwordField() {
        UiKit u = host.ui();
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
        metrics.reset();
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
        host.refreshForegroundService();
        host.log("warn", "Send stop requested");
        host.requestRender();
    }

    void shutdown() {
        GoncBridge.Session current = session;
        session = null;
        if (current != null) {
            current.stop();
        }
    }

    void endTask() {
        runId++;
        shutdown();
    }

    void resetForFreshLaunch() {
        shareItems.clear();
        useUdp = false;
        passwordVisible = false;
        password = Passwords.generate();
        status = "Idle";
        passwordVisibilityToken++;
        metrics.reset();
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
                    host.requestBackgroundRender();
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
