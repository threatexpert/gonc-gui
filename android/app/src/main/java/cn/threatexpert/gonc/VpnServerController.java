package cn.threatexpert.gonc;

import android.graphics.Color;
import android.graphics.Typeface;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.View;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * VPN server ("link agent") module: owns its own state, view building and gonc
 * session. Independent of the file-transfer modules; it reaches shared services
 * (render, logging, QR, clipboard, metrics) through {@link ModuleHost}.
 */
final class VpnServerController {
    private ModuleHost host;
    private final TransferMetrics metrics = new TransferMetrics();

    private boolean useUdp;
    private boolean passwordVisible;
    private int passwordVisibilityToken;
    private String password = Passwords.generate();
    private String status = "Idle";
    private GoncBridge.Session session;
    private long runId;

    VpnServerController(ModuleHost host) {
        this.host = host;
    }

    /** Rebind to the current host after an Activity recreation (config change). */
    void attach(ModuleHost host) {
        this.host = host;
    }

    boolean isRunning() {
        return session != null;
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
        passwordVisible = false;
        password = Passwords.generate();
        status = "Idle";
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

        card.addView(u.sectionBoundaryTitle(string(R.string.passphrase_config), false), u.blockParams(0));
        TextView intro = u.text(string(R.string.vpn_server_intro), 13, u.muted(), Typeface.NORMAL);
        intro.setSingleLine(false);
        card.addView(intro, u.blockParams(u.dp(6)));
        card.addView(passwordField());
        card.addView(u.sectionDivider(), u.dividerParams(u.dp(12)));
        card.addView(protocolToggle());

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
        metrics.reset();
        status = "Preparing";
        host.log("info", "Start VPN server requested");
        long id = ++runId;
        session = host.bridge().startP2PLinkAgent(host.context(), passphrase, useUdp, callback(id));
        host.updateKeepScreenOn();
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
        host.updateKeepScreenOn();
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
                    host.updateKeepScreenOn();
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
                    host.updateKeepScreenOn();
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
