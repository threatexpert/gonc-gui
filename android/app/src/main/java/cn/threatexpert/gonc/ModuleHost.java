package cn.threatexpert.gonc;

import android.content.Context;
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

    /** Full rebuild of the visible screen. */
    void requestRender();

    /** Throttled rebuild for high-frequency background updates (logs/metrics). */
    void requestBackgroundRender();

    void updateKeepScreenOn();

    void copyText(String label, String value);

    void showPassphraseQr(String passphrase);

    /** Launch the QR scanner; {@code onResult} receives the scanned text on the main thread. */
    void scanPassphrase(Consumer<String> onResult);

    void toast(int messageResId);

    void updateMetricsFromLog(TransferMetrics metrics, String message);

    void updateMetricsFromReport(TransferMetrics metrics, String topic, String status, String network, String mode, String peer);
}
