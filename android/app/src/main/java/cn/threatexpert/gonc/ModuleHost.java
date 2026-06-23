package cn.threatexpert.gonc;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.Handler;

import java.util.function.Consumer;

/**
 * Shared services a per-module panel/controller calls back into. Implemented by
 * {@code MainActivity}, which remains the host: it owns mode routing, the log
 * panel, the activity/metrics chrome, and the QR-scan plumbing. A module talks
 * to the host only through this seam, so each module can be reasoned about and
 * evolved independently of the others.
 */
interface ModuleHost {
    Context context();

    UiKit ui();

    GoncBridge bridge();

    Handler mainHandler();

    void log(String level, String message);

    /** Log without persisting to the crash-diagnostics buffer (already persisted elsewhere). */
    void logTransient(String level, String message);

    SharedPreferences prefs();

    /** Launch the VPN client tunnel (host owns the permission/battery/service flow). */
    void startVpnClient();

    void stopVpnClient();

    /** Open the system file/folder picker for the send module; results flow back via SendController.addFiles/addFolder. */
    void pickSendFiles();

    void pickSendFolder();

    /** Open the system folder picker for the receive save location; result flows back via ReceiveController.setSaveLocation. */
    void pickSaveLocation();

    /**
     * Ensure legacy ({@code WRITE_EXTERNAL_STORAGE}) save permission. Returns
     * {@code true} when writing is already allowed (or not needed on this SDK);
     * {@code false} after launching the permission request, in which case the
     * caller should abort and let the user retry.
     */
    boolean ensureLegacyStoragePermission();

    /** Whether high-frequency auto-render is currently paused (user is interacting with a metric value). */
    boolean isAutoRenderPaused();

    /** Full rebuild of the visible screen. */
    void requestRender();

    /** Throttled rebuild for high-frequency background updates (logs/metrics). */
    void requestBackgroundRender();

    void updateKeepScreenOn();

    void copyText(String label, String value);

    void showPassphraseQr(String passphrase);

    /** Launch the QR scanner; {@code onResult} receives the scanned text on the main thread. */
    void scanPassphrase(Consumer<String> onResult);

    /** Launch the QR scanner with the profile-import prompt. */
    void scanProfileQr(Consumer<String> onResult);

    void toast(int messageResId);

    void updateMetricsFromLog(TransferMetrics metrics, String message);

    void updateMetricsFromReport(TransferMetrics metrics, String topic, String status, String network, String mode, String peer);
}
