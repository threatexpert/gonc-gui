package cn.threatexpert.gonc;

import android.app.AlertDialog;
import android.app.Dialog;
import android.content.SharedPreferences;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.Typeface;
import android.text.Editable;
import android.text.TextWatcher;
import android.text.method.PasswordTransformationMethod;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.widget.AdapterView;
import android.widget.ArrayAdapter;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.ImageButton;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.PopupMenu;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * VPN client module: owns the saved profiles, their persistence, the client
 * view, the {@link GoncVpnState} listener, and metric/log sync. The actual
 * tunnel runtime lives in {@link GoncVpnState}/{@code GoncVpnService}; the
 * Activity-coupled start/stop/permission/battery flow stays in the host
 * ({@link ModuleHost#startVpnClient()} / {@link ModuleHost#stopVpnClient()}),
 * which reads the active profile back through {@link #currentProfile()}.
 */
final class VpnClientController {
    interface TextSink {
        void set(String value);
    }

    private static final String KEY_VPN_PROFILES = "vpn_profiles";
    private static final String KEY_SELECTED_VPN_PROFILE = "selected_vpn_profile";
    private static final String VPN_PROFILE_QR_TYPE = "gonc.vpn.profile";

    private ModuleHost host;
    private final List<VpnProfile> profiles = new ArrayList<>();
    private final TransferMetrics metrics = new TransferMetrics();
    private int selectedIndex;
    private boolean passwordVisible;
    private boolean advancedExpanded;
    private long lastLogId;
    private long lastLogRenderMs;
    private GoncVpnState.Listener listener;

    VpnClientController(ModuleHost host) {
        this.host = host;
    }

    /** Rebind to the current host after an Activity recreation (config change). */
    void attach(ModuleHost host) {
        this.host = host;
    }

    // --- lifecycle --------------------------------------------------------

    /** Load saved profiles. Must be called from onCreate (needs a ready Context). */
    void load() {
        loadProfiles();
    }

    void register() {
        listener = new GoncVpnState.Listener() {
            @Override
            public void onVpnStateChanged() {
                host.mainHandler().post(() -> {
                    host.updateKeepScreenOn();
                    host.requestRender();
                });
            }

            @Override
            public void onVpnLog(GoncVpnState.LogEntry entry) {
                host.mainHandler().post(() -> {
                    appendLog(entry);
                    long now = System.currentTimeMillis();
                    if (now - lastLogRenderMs >= 200) {
                        lastLogRenderMs = now;
                        host.requestBackgroundRender();
                    }
                });
            }
        };
        GoncVpnState.setListener(listener);
        syncLogs();
    }

    void unregister() {
        GoncVpnState.removeListener(listener);
    }

    // --- host-facing accessors -------------------------------------------

    VpnProfile currentProfile() {
        if (profiles.isEmpty()) {
            profiles.add(VpnProfile.defaults(string(R.string.vpn_profile_default_name)));
            selectedIndex = 0;
        }
        selectedIndex = Math.max(0, Math.min(selectedIndex, profiles.size() - 1));
        return profiles.get(selectedIndex);
    }

    void normalizeAndSaveCurrent() {
        normalizeCurrentProfile();
        saveProfiles();
    }

    void resetForFreshLaunch() {
        selectedIndex = 0;
        passwordVisible = false;
    }

    TransferMetrics metrics() {
        metrics.p2pStatus = GoncVpnState.p2pStatus();
        metrics.network = GoncVpnState.network();
        metrics.routeMode = GoncVpnState.route();
        metrics.peer = GoncVpnState.peer();
        return metrics;
    }

    void syncLogs() {
        for (GoncVpnState.LogEntry entry : GoncVpnState.logsAfter(lastLogId)) {
            appendLog(entry);
        }
    }

    private void appendLog(GoncVpnState.LogEntry entry) {
        if (entry == null || entry.id <= lastLogId) {
            return;
        }
        host.logTransient(entry.level, entry.message);
        lastLogId = entry.id;
    }

    // --- view -------------------------------------------------------------

    View panel() {
        UiKit u = host.ui();
        LinearLayout card = u.card();
        boolean running = GoncVpnState.isRunning();
        String error = GoncVpnState.error().trim();
        if (running) {
            card.addView(sessionBarView());
            if (!error.isEmpty()) {
                card.addView(errorBanner(error), u.blockParams(u.dp(8)));
            }
            return card;
        }

        // Surface the last failure (e.g. a quick exit from a bad link/extra-args
        // config) above the form — otherwise the UI just flashes back here silently.
        if (!error.isEmpty()) {
            card.addView(errorBanner(error), u.blockParams(0));
        }

        card.addView(profileSelector(), u.blockParams(error.isEmpty() ? 0 : u.dp(10)));
        card.addView(passphraseField(), u.blockParams(u.dp(8)));
        Button primary = u.primaryButton(string(R.string.vpn_connect));
        primary.setOnClickListener(v -> host.startVpnClient());
        card.addView(primary, u.blockParams(u.dp(12)));

        Button advancedToggle = u.secondaryButton(advancedExpanded
                ? string(R.string.vpn_advanced_settings_hide)
                : string(R.string.vpn_advanced_settings));
        advancedToggle.setOnClickListener(v -> {
            advancedExpanded = !advancedExpanded;
            host.requestRender();
        });
        card.addView(advancedToggle, u.blockParams(u.dp(6)));

        if (advancedExpanded) {
            card.addView(profileNameField(), u.blockParams(u.dp(10)));
            card.addView(options(), u.blockParams(u.dp(10)));
            card.addView(linkConfigField(), u.blockParams(u.dp(10)));
            card.addView(extraArgsField(), u.blockParams(u.dp(10)));
            card.addView(multilineField(
                    string(R.string.vpn_dns_servers),
                    string(R.string.vpn_dns_hint),
                    currentProfile().dnsServers,
                    2,
                    value -> currentProfile().dnsServers = value
            ), u.blockParams(u.dp(10)));
            card.addView(multilineField(
                    string(R.string.vpn_route_cidrs),
                    string(R.string.vpn_route_cidrs_hint),
                    currentProfile().routeCidrs,
                    4,
                    value -> currentProfile().routeCidrs = value
            ), u.blockParams(u.dp(10)));
        }
        return card;
    }

    private View profileSelector() {
        UiKit u = host.ui();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);

        TextView label = u.text(string(R.string.vpn_profile_label), 13, u.muted(), Typeface.BOLD);
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        labelParams.setMargins(0, 0, u.dp(10), 0);
        row.addView(label, labelParams);

        Spinner spinner = new Spinner(host.context());
        ArrayAdapter<String> adapter = new ArrayAdapter<>(host.context(), android.R.layout.simple_spinner_item, profileNames());
        adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        spinner.setAdapter(adapter);
        spinner.setSelection(Math.max(0, Math.min(selectedIndex, profiles.size() - 1)));
        spinner.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        spinner.setPadding(u.dp(10), 0, u.dp(10), 0);
        spinner.setOnItemSelectedListener(new AdapterView.OnItemSelectedListener() {
            @Override
            public void onItemSelected(AdapterView<?> parent, View view, int position, long id) {
                if (position != selectedIndex && position >= 0 && position < profiles.size()) {
                    selectedIndex = position;
                    saveSelectedProfile();
                    passwordVisible = false;
                    host.requestRender();
                }
            }

            @Override
            public void onNothingSelected(AdapterView<?> parent) {
            }
        });
        row.addView(spinner, new LinearLayout.LayoutParams(0, u.dp(46), 1));

        Button menuBtn = u.secondaryButton("⋮");
        menuBtn.setTextSize(16);
        menuBtn.setOnClickListener(v -> {
            PopupMenu popup = new PopupMenu(host.context(), menuBtn);
            popup.getMenu().add(0, 1, 0, string(R.string.vpn_profile_new));
            popup.getMenu().add(0, 2, 1, string(R.string.vpn_profile_import));
            popup.getMenu().add(0, 3, 2, string(R.string.vpn_profile_export));
            popup.getMenu().add(0, 4, 3, string(R.string.vpn_profile_delete));
            popup.setOnMenuItemClickListener(item -> {
                switch (item.getItemId()) {
                    case 1:
                        profiles.add(VpnProfile.defaults(uniqueProfileName()));
                        selectedIndex = profiles.size() - 1;
                        saveProfiles();
                        passwordVisible = false;
                        host.requestRender();
                        return true;
                    case 2:
                        scanProfile();
                        return true;
                    case 3:
                        showProfileQr();
                        return true;
                    case 4:
                        String profileName = currentProfile().displayName(host.context());
                        new AlertDialog.Builder(host.context())
                                .setMessage(string(R.string.vpn_profile_delete_confirm, profileName))
                                .setPositiveButton(string(R.string.vpn_profile_delete), (dialog, which) -> {
                                    if (profiles.size() > 1) {
                                        profiles.remove(selectedIndex);
                                        selectedIndex = Math.max(0, selectedIndex - 1);
                                    } else {
                                        profiles.set(0, VpnProfile.defaults(string(R.string.vpn_profile_default_name)));
                                        selectedIndex = 0;
                                    }
                                    saveProfiles();
                                    passwordVisible = false;
                                    host.requestRender();
                                })
                                .setNegativeButton(string(R.string.cancel), null)
                                .show();
                        return true;
                    default:
                        return false;
                }
            });
            popup.show();
        });
        LinearLayout.LayoutParams menuParams = new LinearLayout.LayoutParams(u.dp(46), u.dp(46));
        menuParams.setMargins(u.dp(8), 0, 0, 0);
        row.addView(menuBtn, menuParams);
        return row;
    }

    private View profileNameField() {
        return labeledSingleLineField(
                string(R.string.vpn_profile_name),
                currentProfile().name,
                string(R.string.vpn_profile_name_hint),
                value -> currentProfile().name = value
        );
    }

    private View passphraseField() {
        UiKit u = host.ui();
        LinearLayout box = u.column();
        box.addView(u.text(string(R.string.passphrase), 13, u.muted(), Typeface.BOLD));
        LinearLayout line = u.row();
        EditText input = new EditText(host.context());
        input.setSingleLine(true);
        input.setText(currentProfile().passphrase);
        input.setTextColor(u.ink());
        input.setTextSize(15);
        input.setHint(string(R.string.vpn_passphrase_hint));
        input.setHintTextColor(Color.rgb(148, 163, 184));
        input.setPadding(u.dp(12), 0, u.dp(12), 0);
        input.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        input.setTransformationMethod(passwordVisible ? null : PasswordTransformationMethod.getInstance());
        input.addTextChangedListener(simpleWatcher(value -> currentProfile().passphrase = value));
        line.addView(input, new LinearLayout.LayoutParams(0, u.dp(46), 1));

        ImageButton visibility = new ImageButton(host.context());
        visibility.setImageResource(android.R.drawable.ic_menu_view);
        visibility.setBackground(u.rounded(Color.TRANSPARENT, u.dp(6), Color.rgb(203, 215, 230), 1));
        visibility.setContentDescription(string(passwordVisible ? R.string.hide : R.string.show));
        visibility.setOnClickListener(v -> {
            passwordVisible = !passwordVisible;
            host.requestRender();
        });
        LinearLayout.LayoutParams visibilityParams = new LinearLayout.LayoutParams(u.dp(46), u.dp(46));
        visibilityParams.setMargins(u.dp(8), 0, 0, 0);
        line.addView(visibility, visibilityParams);

        Button scan = u.secondaryButton(string(R.string.scan));
        scan.setOnClickListener(v -> scanPassphrase());
        LinearLayout.LayoutParams scanParams = new LinearLayout.LayoutParams(u.dp(76), u.dp(46));
        scanParams.setMargins(u.dp(8), 0, 0, 0);
        line.addView(scan, scanParams);
        box.addView(line, u.blockParams(u.dp(4)));
        return box;
    }

    private View options() {
        UiKit u = host.ui();
        LinearLayout box = u.column();
        boolean running = GoncVpnState.isRunning();
        CheckBox udp = new CheckBox(host.context());
        udp.setText(string(R.string.use_udp_protocol));
        udp.setTextColor(Color.rgb(64, 81, 105));
        udp.setTextSize(14);
        udp.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        udp.setChecked(currentProfile().useUdp);
        u.setControlEnabled(udp, !running);
        udp.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (!GoncVpnState.isRunning()) {
                currentProfile().useUdp = isChecked;
            }
        });
        box.addView(udp);

        CheckBox ipv6 = new CheckBox(host.context());
        ipv6.setText(string(R.string.vpn_route_ipv6));
        ipv6.setTextColor(Color.rgb(64, 81, 105));
        ipv6.setTextSize(14);
        ipv6.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        ipv6.setChecked(currentProfile().routeIpv6);
        u.setControlEnabled(ipv6, !running);
        ipv6.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (!GoncVpnState.isRunning()) {
                currentProfile().routeIpv6 = isChecked;
            }
        });
        box.addView(ipv6);
        TextView hint = u.text(string(R.string.vpn_route_ipv6_hint), 12, u.muted(), Typeface.NORMAL);
        hint.setPadding(u.dp(4), 0, 0, 0);
        box.addView(hint);

        CheckBox tunnelOnly = new CheckBox(host.context());
        tunnelOnly.setText(string(R.string.vpn_tunnel_only));
        tunnelOnly.setTextColor(Color.rgb(64, 81, 105));
        tunnelOnly.setTextSize(14);
        tunnelOnly.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        tunnelOnly.setChecked(currentProfile().tunnelOnly);
        u.setControlEnabled(tunnelOnly, !running);
        tunnelOnly.setOnCheckedChangeListener((buttonView, isChecked) -> {
            if (!GoncVpnState.isRunning()) {
                currentProfile().tunnelOnly = isChecked;
            }
        });
        LinearLayout.LayoutParams tunnelOnlyParams = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        tunnelOnlyParams.setMargins(0, u.dp(6), 0, 0);
        box.addView(tunnelOnly, tunnelOnlyParams);
        TextView tunnelOnlyHint = u.text(string(R.string.vpn_tunnel_only_hint), 12, u.muted(), Typeface.NORMAL);
        tunnelOnlyHint.setPadding(u.dp(4), 0, 0, 0);
        box.addView(tunnelOnlyHint);
        return box;
    }

    private View linkConfigField() {
        UiKit u = host.ui();
        LinearLayout box = (LinearLayout) labeledSingleLineField(
                string(R.string.vpn_link_config),
                currentProfile().linkConfig,
                string(R.string.vpn_link_config_hint),
                value -> currentProfile().linkConfig = value);
        TextView desc = u.text(string(R.string.vpn_link_config_desc), 12, u.muted(), Typeface.NORMAL);
        desc.setPadding(u.dp(4), u.dp(4), 0, 0);
        box.addView(desc);
        return box;
    }

    private View extraArgsField() {
        UiKit u = host.ui();
        LinearLayout box = (LinearLayout) labeledSingleLineField(
                string(R.string.vpn_extra_args),
                currentProfile().extraArgs,
                string(R.string.vpn_extra_args_hint),
                value -> currentProfile().extraArgs = value);
        TextView desc = u.text(string(R.string.vpn_extra_args_desc), 12, u.muted(), Typeface.NORMAL);
        desc.setPadding(u.dp(4), u.dp(4), 0, 0);
        box.addView(desc);
        return box;
    }

    private View labeledSingleLineField(String label, String value, String hint, TextSink sink) {
        UiKit u = host.ui();
        LinearLayout box = u.column();
        box.addView(u.text(label, 13, u.muted(), Typeface.BOLD));
        EditText input = new EditText(host.context());
        input.setSingleLine(true);
        input.setText(value == null ? "" : value);
        input.setTextColor(u.ink());
        input.setTextSize(15);
        input.setHint(hint);
        input.setHintTextColor(Color.rgb(148, 163, 184));
        input.setPadding(u.dp(12), 0, u.dp(12), 0);
        input.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        input.addTextChangedListener(simpleWatcher(sink));
        box.addView(input, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, u.dp(46)));
        return box;
    }

    private View multilineField(String label, String hint, String value, int minLines, TextSink sink) {
        UiKit u = host.ui();
        LinearLayout box = u.column();
        box.addView(u.text(label, 13, u.muted(), Typeface.BOLD));
        EditText input = new EditText(host.context());
        input.setSingleLine(false);
        input.setMinLines(minLines);
        input.setText(value == null ? "" : value);
        input.setTextColor(u.ink());
        input.setTextSize(14);
        input.setHint(hint);
        input.setHintTextColor(Color.rgb(148, 163, 184));
        input.setPadding(u.dp(12), u.dp(8), u.dp(12), u.dp(8));
        input.setGravity(Gravity.TOP);
        input.setBackground(u.rounded(Color.WHITE, u.dp(6), Color.rgb(203, 215, 230), 1));
        input.addTextChangedListener(simpleWatcher(sink));
        box.addView(input, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        return box;
    }

    private View sessionBarView() {
        UiKit u = host.ui();
        LinearLayout row = u.row();
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(u.dp(10), u.dp(8), u.dp(10), u.dp(8));
        row.setBackground(u.rounded(Color.rgb(248, 251, 255), u.dp(8), Color.rgb(226, 232, 240), 1));

        View dot = new View(host.context());
        dot.setBackground(u.rounded(connectionColor(), u.dp(6), 0, 0));
        row.addView(dot, new LinearLayout.LayoutParams(u.dp(12), u.dp(12)));

        String activeProfile = GoncVpnState.profileName();
        if (activeProfile.trim().isEmpty()) {
            activeProfile = currentProfile().displayName(host.context());
        }
        TextView label = u.text(connectionLabel() + "\n" + string(R.string.vpn_active_profile, activeProfile), 14, u.ink(), Typeface.BOLD);
        label.setSingleLine(false);
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        labelParams.setMargins(u.dp(10), 0, u.dp(8), 0);
        row.addView(label, labelParams);

        Button disconnect = u.dangerButton(string(R.string.disconnect));
        disconnect.setTextSize(14);
        disconnect.setOnClickListener(v -> host.stopVpnClient());
        u.setControlEnabled(disconnect, !GoncVpnState.STOPPING.equals(GoncVpnState.status()));
        LinearLayout.LayoutParams disconnectParams = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, u.dp(38));
        disconnectParams.setMargins(u.dp(8), 0, 0, 0);
        row.addView(disconnect, disconnectParams);
        return row;
    }

    private View errorBanner(String message) {
        UiKit u = host.ui();
        TextView view = u.text(message, 13, Color.rgb(176, 42, 42), Typeface.BOLD);
        view.setSingleLine(false);
        view.setPadding(u.dp(10), u.dp(8), u.dp(10), u.dp(8));
        view.setBackground(u.rounded(Color.rgb(253, 242, 242), u.dp(8), Color.rgb(201, 63, 63), 1));
        return view;
    }

    private String connectionLabel() {
        // Same shared interpretation as the receive module, off the same gonc P2P report.
        return host.ui().connectionLabel(metrics());
    }

    private int connectionColor() {
        return host.ui().connectionColor(host.ui().connectionState(metrics()));
    }

    // --- scan + QR --------------------------------------------------------

    private void scanPassphrase() {
        if (GoncVpnState.isRunning()) {
            return;
        }
        host.scanPassphrase(result -> {
            if (GoncVpnState.isRunning()) {
                return;
            }
            currentProfile().passphrase = result.trim();
            host.log("info", "VPN passphrase scanned");
            host.requestRender();
        });
    }

    private void scanProfile() {
        if (GoncVpnState.isRunning()) {
            return;
        }
        host.scanProfileQr(this::importProfileFromQr);
    }

    private void importProfileFromQr(String value) {
        try {
            JSONObject root = new JSONObject(value);
            if (!VPN_PROFILE_QR_TYPE.equals(root.optString("type"))) {
                host.toast(R.string.toast_vpn_profile_qr_invalid);
                return;
            }
            JSONObject profileJson = root.optJSONObject("profile");
            if (profileJson == null) {
                host.toast(R.string.toast_vpn_profile_qr_invalid);
                return;
            }
            VpnProfile profile = VpnProfile.fromJson(profileJson, host.context());
            profile.name = importedProfileName(profile.displayName(host.context()));
            profiles.add(profile);
            selectedIndex = profiles.size() - 1;
            passwordVisible = false;
            saveProfiles();
            host.log("info", "VPN profile imported: " + profile.displayName(host.context()));
            Toast.makeText(host.context(), string(R.string.toast_vpn_profile_imported, profile.displayName(host.context())), Toast.LENGTH_SHORT).show();
            host.requestRender();
        } catch (JSONException error) {
            host.toast(R.string.toast_vpn_profile_qr_invalid);
        }
    }

    private void showProfileQr() {
        UiKit u = host.ui();
        VpnProfile profile = currentProfile();
        normalizeCurrentProfile();
        saveProfiles();
        try {
            JSONObject root = new JSONObject();
            root.put("type", VPN_PROFILE_QR_TYPE);
            root.put("version", 1);
            root.put("profile", profile.toJson());
            String payload = root.toString();

            LinearLayout layout = u.column();
            layout.setPadding(u.dp(18), u.dp(18), u.dp(18), u.dp(8));
            layout.setBackground(u.rounded(Color.WHITE, u.dp(8), 0, 0));
            TextView title = u.text(profile.displayName(host.context()), 16, u.ink(), Typeface.BOLD);
            title.setGravity(Gravity.CENTER);
            layout.addView(title);

            Bitmap bitmap = QrCodes.encode(payload, u.dp(260));
            ImageView image = new ImageView(host.context());
            image.setImageBitmap(bitmap);
            image.setAdjustViewBounds(true);
            LinearLayout.LayoutParams imageParams = new LinearLayout.LayoutParams(u.dp(260), u.dp(260));
            imageParams.gravity = Gravity.CENTER_HORIZONTAL;
            imageParams.setMargins(0, u.dp(12), 0, 0);
            layout.addView(image, imageParams);

            TextView hint = u.text(string(R.string.vpn_profile_qr_hint), 12, u.muted(), Typeface.NORMAL);
            hint.setGravity(Gravity.CENTER);
            layout.addView(hint, u.blockParams(u.dp(8)));

            Dialog dialog = new Dialog(host.context());
            dialog.requestWindowFeature(Window.FEATURE_NO_TITLE);
            dialog.setContentView(layout);
            Window window = dialog.getWindow();
            if (window != null) {
                window.setBackgroundDrawable(u.rounded(Color.WHITE, u.dp(10), 0, 0));
            }
            dialog.show();
        } catch (Exception error) {
            host.toast(R.string.toast_qr_failed);
        }
    }

    // --- profiles persistence --------------------------------------------

    private void loadProfiles() {
        SharedPreferences prefs = host.prefs();
        String raw = prefs.getString(KEY_VPN_PROFILES, "");
        if (raw != null && !raw.trim().isEmpty()) {
            try {
                JSONArray array = new JSONArray(raw);
                for (int i = 0; i < array.length(); i++) {
                    JSONObject object = array.optJSONObject(i);
                    if (object != null) {
                        profiles.add(VpnProfile.fromJson(object, host.context()));
                    }
                }
            } catch (JSONException error) {
                GoncCrashReporter.appendLog(host.context(), "warn", "Cannot load VPN profiles: " + error.getMessage());
            }
        }
        if (profiles.isEmpty()) {
            profiles.add(VpnProfile.defaults(string(R.string.vpn_profile_default_name)));
        }
        selectedIndex = prefs.getInt(KEY_SELECTED_VPN_PROFILE, 0);
        selectedIndex = Math.max(0, Math.min(selectedIndex, profiles.size() - 1));
    }

    private void saveProfiles() {
        normalizeCurrentProfile();
        JSONArray array = new JSONArray();
        for (VpnProfile profile : profiles) {
            array.put(profile.toJson());
        }
        host.prefs()
                .edit()
                .putString(KEY_VPN_PROFILES, array.toString())
                .putInt(KEY_SELECTED_VPN_PROFILE, selectedIndex)
                .apply();
    }

    private void saveSelectedProfile() {
        host.prefs()
                .edit()
                .putInt(KEY_SELECTED_VPN_PROFILE, selectedIndex)
                .apply();
    }

    private List<String> profileNames() {
        List<String> names = new ArrayList<>();
        for (VpnProfile profile : profiles) {
            names.add(profile.displayName(host.context()));
        }
        return names;
    }

    private String uniqueProfileName() {
        return uniqueProfileName(string(R.string.vpn_profile_new_name));
    }

    private String importedProfileName(String name) {
        String base = name == null || name.trim().isEmpty()
                ? string(R.string.vpn_profile_default_name)
                : name.trim();
        return uniqueProfileName(base);
    }

    private String uniqueProfileName(String base) {
        Set<String> existing = new HashSet<>();
        for (VpnProfile profile : profiles) {
            existing.add(profile.displayName(host.context()));
        }
        if (!existing.contains(base)) {
            return base;
        }
        for (int i = 2; i < 1000; i++) {
            String candidate = base + " " + i;
            if (!existing.contains(candidate)) {
                return candidate;
            }
        }
        return base + " " + System.currentTimeMillis();
    }

    private void normalizeCurrentProfile() {
        VpnProfile profile = currentProfile();
        if (profile.name == null || profile.name.trim().isEmpty()) {
            profile.name = string(R.string.vpn_profile_default_name);
        } else {
            profile.name = profile.name.trim();
        }
        profile.passphrase = profile.passphrase == null ? "" : profile.passphrase.trim();
        profile.linkConfig = profile.linkConfig == null ? "" : profile.linkConfig.trim();
        profile.extraArgs = profile.extraArgs == null ? "" : profile.extraArgs.trim();
        profile.dnsServers = normalizeLines(profile.dnsServers);
        profile.routeCidrs = normalizeLines(profile.routeCidrs);
    }

    private String normalizeLines(String value) {
        if (value == null || value.trim().isEmpty()) {
            return "";
        }
        StringBuilder builder = new StringBuilder();
        for (String line : value.split("\\r?\\n")) {
            String clean = line.trim();
            if (clean.isEmpty()) {
                continue;
            }
            if (builder.length() > 0) {
                builder.append('\n');
            }
            builder.append(clean);
        }
        return builder.toString();
    }

    private TextWatcher simpleWatcher(TextSink sink) {
        return new TextWatcher() {
            @Override
            public void beforeTextChanged(CharSequence s, int start, int count, int after) {
            }

            @Override
            public void onTextChanged(CharSequence s, int start, int before, int count) {
                sink.set(s == null ? "" : s.toString());
            }

            @Override
            public void afterTextChanged(Editable s) {
            }
        };
    }

    private String string(int resId) {
        return host.context().getString(resId);
    }

    private String string(int resId, Object... args) {
        return host.context().getString(resId, args);
    }
}
