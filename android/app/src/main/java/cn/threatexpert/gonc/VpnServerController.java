package cn.threatexpert.gonc;

import android.app.AlertDialog;
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

import org.json.JSONArray;
import org.json.JSONException;

import java.util.ArrayList;
import java.util.List;

/**
 * VPN server ("link agent") module: owns its own state, view building and gonc
 * session. Independent of the file-transfer modules; it reaches shared services
 * (render, logging, QR, clipboard, metrics) through {@link ModuleHost}.
 */
final class VpnServerController {
    private static final String KEY_SERVER_EXTRA_ARGS_HISTORY = "vpn_server_extra_args_history";
    private static final int MAX_EXTRA_ARGS_HISTORY = 5;

    private ModuleHost host;
    private final TransferMetrics metrics = new TransferMetrics();
    private final List<String> extraArgsHistory = new ArrayList<>();

    private boolean useUdp;
    private boolean advancedExpanded;
    private String extraArgs = "";
    private boolean passwordVisible;
    private int passwordVisibilityToken;
    private String password = Passwords.generate();
    private String status = "Idle";
    private String errorMessage;
    private GoncBridge.Session session;
    private long runId;

    VpnServerController(ModuleHost host) {
        this.host = host;
    }

    /** Rebind to the current host after an Activity recreation (config change). */
    void attach(ModuleHost host) {
        this.host = host;
    }

    /**
     * Load the extra-args history. Must be called from onCreate (needs a ready Context).
     * The current value is intentionally NOT restored — only the picker history is.
     */
    void load() {
        extraArgsHistory.clear();
        String raw = host.prefs().getString(KEY_SERVER_EXTRA_ARGS_HISTORY, "");
        if (raw == null || raw.trim().isEmpty()) {
            return;
        }
        try {
            JSONArray array = new JSONArray(raw);
            for (int i = 0; i < array.length() && extraArgsHistory.size() < MAX_EXTRA_ARGS_HISTORY; i++) {
                String value = array.optString(i, "").trim();
                if (!value.isEmpty() && !extraArgsHistory.contains(value)) {
                    extraArgsHistory.add(value);
                }
            }
        } catch (JSONException ignored) {
        }
    }

    /** Record a used value as the most recent history entry (deduped, capped). */
    private void addToHistory(String value) {
        String clean = value == null ? "" : value.trim();
        if (clean.isEmpty()) {
            return;
        }
        extraArgsHistory.remove(clean);
        extraArgsHistory.add(0, clean);
        while (extraArgsHistory.size() > MAX_EXTRA_ARGS_HISTORY) {
            extraArgsHistory.remove(extraArgsHistory.size() - 1);
        }
        saveHistory();
    }

    private void removeFromHistory(String value) {
        if (extraArgsHistory.remove(value)) {
            saveHistory();
        }
    }

    private void saveHistory() {
        JSONArray array = new JSONArray();
        for (String item : extraArgsHistory) {
            array.put(item);
        }
        host.prefs().edit().putString(KEY_SERVER_EXTRA_ARGS_HISTORY, array.toString()).apply();
    }

    boolean isRunning() {
        return session != null;
    }

    /** Foreground-service contribution: null when idle; dot is green once a client is connected. */
    GoncForegroundService.State foregroundState() {
        if (session == null) {
            return null;
        }
        GoncForegroundService.Dot dot = metrics.connectedCount > 0
                ? GoncForegroundService.Dot.GREEN
                : GoncForegroundService.Dot.YELLOW;
        return new GoncForegroundService.State(dot, -1);
    }

    /** Stop the session quietly (no UI/log), e.g. on Activity destroy. */
    void shutdown() {
        GoncBridge.Session current = session;
        session = null;
        if (current != null) {
            current.stop();
        }
    }

    /** Invalidate the running session and stop it, e.g. when ending all tasks. */
    void endTask() {
        runId++;
        shutdown();
    }

    /** Reset transient state for a fresh launch. */
    void resetForFreshLaunch() {
        useUdp = false;
        advancedExpanded = false;
        extraArgs = "";
        passwordVisible = false;
        password = Passwords.generate();
        status = "Idle";
        errorMessage = null;
        passwordVisibilityToken++;
        metrics.reset();
    }

    TransferMetrics metrics() {
        return metrics;
    }

    String status() {
        return status;
    }

    View panel() {
        UiKit u = host.ui();
        LinearLayout card = u.card();

        if (session == null && errorMessage != null && !errorMessage.trim().isEmpty()) {
            card.addView(errorBanner(string(R.string.vpn_server_exit_error) + "\n" + errorMessage), u.blockParams(0));
        }

        card.addView(u.sectionBoundaryTitle(string(R.string.passphrase_config), false), u.blockParams(session == null && errorMessage != null && !errorMessage.trim().isEmpty() ? u.dp(10) : 0));
        TextView intro = u.text(string(R.string.vpn_server_intro), 13, u.muted(), Typeface.NORMAL);
        intro.setSingleLine(false);
        card.addView(intro, u.blockParams(u.dp(6)));
        card.addView(passwordField());

        Button advancedToggle = u.secondaryButton(advancedExpanded
                ? string(R.string.vpn_advanced_settings_hide)
                : string(R.string.vpn_advanced_settings));
        advancedToggle.setOnClickListener(v -> {
            advancedExpanded = !advancedExpanded;
            host.requestRender();
        });
        card.addView(advancedToggle, u.blockParams(u.dp(12)));

        if (advancedExpanded) {
            card.addView(u.sectionDivider(), u.dividerParams(u.dp(12)));
            card.addView(protocolToggle(), u.blockParams(u.dp(10)));
            card.addView(extraArgsField(), u.blockParams(u.dp(10)));
        }

        Button primary = session == null
                ? u.primaryButton(string(R.string.start_vpn_server))
                : u.dangerButton(string(R.string.stop_vpn_server));
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

    private View errorBanner(String message) {
        UiKit u = host.ui();
        TextView view = u.text(message, 13, Color.rgb(176, 42, 42), Typeface.BOLD);
        view.setSingleLine(false);
        view.setPadding(u.dp(10), u.dp(8), u.dp(10), u.dp(8));
        view.setBackground(u.rounded(Color.rgb(253, 242, 242), u.dp(8), Color.rgb(201, 63, 63), 1));
        return view;
    }

    private View extraArgsField() {
        UiKit u = host.ui();
        boolean locked = session != null;
        LinearLayout box = u.column();
        box.addView(u.text(string(R.string.vpn_extra_args), 13, u.muted(), Typeface.BOLD));

        LinearLayout line = u.row();
        EditText input = new EditText(host.context());
        input.setSingleLine(true);
        input.setText(extraArgs);
        input.setTextColor(u.ink());
        input.setTextSize(15);
        input.setHint(string(R.string.vpn_extra_args_hint));
        input.setHintTextColor(Color.rgb(148, 163, 184));
        input.setPadding(u.dp(12), 0, u.dp(12), 0);
        input.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        input.setEnabled(!locked);
        input.addTextChangedListener(new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
                if (session == null) {
                    extraArgs = s == null ? "" : s.toString();
                }
            }

            @Override
            public void afterTextChanged(Editable s) {
            }
        });
        line.addView(input, new LinearLayout.LayoutParams(0, u.dp(46), 1));

        Button history = u.secondaryButton(string(R.string.vpn_extra_args_history));
        u.setControlEnabled(history, !locked && !extraArgsHistory.isEmpty());
        history.setOnClickListener(v -> showExtraArgsHistory());
        LinearLayout.LayoutParams historyParams = new LinearLayout.LayoutParams(LinearLayout.LayoutParams.WRAP_CONTENT, u.dp(46));
        historyParams.setMargins(u.dp(8), 0, 0, 0);
        line.addView(history, historyParams);
        box.addView(line, u.blockParams(u.dp(4)));

        TextView desc = u.text(string(R.string.vpn_extra_args_desc), 12, u.muted(), Typeface.NORMAL);
        desc.setPadding(u.dp(4), u.dp(4), 0, 0);
        box.addView(desc);
        return box;
    }

    private void showExtraArgsHistory() {
        if (session != null || extraArgsHistory.isEmpty()) {
            return;
        }
        UiKit u = host.ui();
        LinearLayout content = u.column();
        content.setPadding(u.dp(18), u.dp(8), u.dp(10), u.dp(8));
        AlertDialog dialog = new AlertDialog.Builder(host.context())
                .setTitle(string(R.string.vpn_extra_args_history))
                .setView(content)
                .setNegativeButton(string(R.string.close), null)
                .create();
        populateHistoryRows(content, dialog);
        dialog.show();
    }

    private void populateHistoryRows(LinearLayout content, AlertDialog dialog) {
        UiKit u = host.ui();
        content.removeAllViews();
        if (extraArgsHistory.isEmpty()) {
            dialog.dismiss();
            host.requestRender();
            return;
        }
        for (String value : new ArrayList<>(extraArgsHistory)) {
            LinearLayout row = u.row();
            row.setGravity(Gravity.CENTER_VERTICAL);

            TextView label = u.text(value, 14, u.ink(), Typeface.NORMAL);
            label.setSingleLine(true);
            label.setPadding(u.dp(2), u.dp(10), u.dp(8), u.dp(10));
            label.setOnClickListener(v -> {
                if (session == null) {
                    extraArgs = value;
                    host.requestRender();
                }
                dialog.dismiss();
            });
            row.addView(label, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

            Button delete = u.ghostButton(string(R.string.vpn_profile_delete));
            delete.setOnClickListener(v -> {
                removeFromHistory(value);
                populateHistoryRows(content, dialog);
            });
            row.addView(delete, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(40)));
            content.addView(row);
        }
    }

    private void applyPasswordVisibility(EditText input) {
        input.setTransformationMethod(passwordVisible ? null : PasswordTransformationMethod.getInstance());
        input.setSelection(input.getText().length());
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
        revealPasswordTemporarily(null);
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
        host.scanPassphrase(this::onScannedPassphrase);
    }

    private void onScannedPassphrase(String result) {
        if (session != null) {
            return;
        }
        password = result.trim();
        revealPasswordTemporarily(null);
        host.log("info", "VPN server passphrase scanned");
        host.requestRender();
    }

    private void start() {
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
        errorMessage = null;
        addToHistory(extraArgs);
        metrics.reset();
        status = "Preparing";
        host.log("info", "Start VPN server requested");
        long id = ++runId;
        session = host.bridge().startP2PLinkAgent(host.context(), passphrase, useUdp, extraArgs, callback(id));
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
        host.log("warn", "VPN server stop requested");
        host.requestRender();
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
            public void onP2PReport(String topic, String reportStatus, String network, String mode, String peer, long timestamp, long pid) {
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
            public void onReady(String endpoint) {
                host.mainHandler().post(() -> {
                    if (runId != id || session == null) {
                        return;
                    }
                    status = "Ready";
                    host.log("info", "VPN server ready, waiting for clients");
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
                    // note: a clean stop leaves no errorMessage to clear (set only on onError)
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
                    errorMessage = error.getMessage() == null ? error.toString() : error.getMessage();
                    host.refreshForegroundService();
                    host.log("error", errorMessage);
                    host.requestRender();
                });
            }
        };
    }

    private String string(int resId) {
        return host.context().getString(resId);
    }
}
