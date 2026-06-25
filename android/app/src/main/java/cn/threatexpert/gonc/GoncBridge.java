package cn.threatexpert.gonc;

import android.content.Context;

import java.util.List;

interface GoncBridge {
    Session startP2PShare(Context context, List<ShareItem> items, String password, boolean useUdp, EventCallback callback);

    Session startP2PReceive(Context context, String password, boolean useUdp, EventCallback callback);

    Session startP2PLinkAgent(Context context, String password, boolean useUdp, String upstream, String dnsForward, String extraArgs, EventCallback callback);

    interface Session {
        void stop();

        void updateShareItems(List<ShareItem> items);
    }

    interface EventCallback {
        void onEvent(String level, String message);

        void onP2PReport(String topic, String status, String network, String mode, String peer, long timestamp, long pid);

        void onReady(String endpoint);

        void onStopped();

        void onError(Throwable error);
    }
}
