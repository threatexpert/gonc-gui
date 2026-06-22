package cn.threatexpert.gonc;

import android.content.Context;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

final class PreviewGoncBridge implements GoncBridge {
    private final ExecutorService executor = Executors.newSingleThreadExecutor();

    @Override
    public Session startP2PShare(Context context, List<ShareItem> items, String password, boolean useUdp, EventCallback callback) {
        PreviewSession session = new PreviewSession();
        List<ShareItem> snapshot = new ArrayList<>(items);
        executor.execute(() -> {
            try {
                callback.onEvent("info", "Preparing " + snapshot.size() + " shared file(s)");
                FileCache.prepareSharedFiles(context, snapshot, (done, total, item, bytes) -> {
                    if (!session.isStopped()) {
                        callback.onEvent("info", "Cached " + done + "/" + total + ": " + item.displayName() + " (" + bytes + " bytes)");
                    }
                });
                if (session.isStopped()) {
                    callback.onStopped();
                    return;
                }
                callback.onEvent("info", "Android P2P share entry is ready.");
                callback.onReady("preview://share-ready");
            } catch (Throwable error) {
                if (!session.isStopped()) {
                    callback.onError(error);
                }
            }
        });
        return session;
    }

    @Override
    public Session startP2PReceive(Context context, String password, boolean useUdp, EventCallback callback) {
        PreviewSession session = new PreviewSession();
        executor.execute(() -> {
            if (session.isStopped()) {
                callback.onStopped();
                return;
            }
            callback.onEvent("info", "P2P receive screen is ready.");
            callback.onReady("preview://receive-ready");
        });
        return session;
    }

    @Override
    public Session startP2PLinkAgent(Context context, String password, boolean useUdp, EventCallback callback) {
        PreviewSession session = new PreviewSession();
        executor.execute(() -> {
            if (session.isStopped()) {
                callback.onStopped();
                return;
            }
            callback.onEvent("info", "P2P linkagent (VPN server) screen is ready.");
            callback.onReady("preview://linkagent-ready");
        });
        return session;
    }

    private static final class PreviewSession implements Session {
        private volatile boolean stopped;

        @Override
        public void stop() {
            stopped = true;
        }

        @Override
        public void updateShareItems(List<ShareItem> items) {
        }

        boolean isStopped() {
            return stopped;
        }
    }
}
