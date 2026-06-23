package cn.threatexpert.gonc;

import android.content.Context;

import java.util.ArrayList;
import java.util.List;

import mobilegonc.Callback;
import mobilegonc.Mobilegonc;

final class MobileGoncBridge implements GoncBridge {
    @Override
    public Session startP2PShare(Context context, List<ShareItem> items, String password, boolean useUdp, EventCallback callback) {
        BridgeSession session = new BridgeSession();
        List<ShareItem> snapshot = new ArrayList<>(items);
        Thread worker = new Thread(() -> {
            try {
                AndroidFileSource source = new AndroidFileSource(context, snapshot);
                session.attachSource(source);
                if (session.isStopped()) {
                    callback.onStopped();
                    return;
                }

                callback.onEvent("info", "Starting gonc mobile share engine with " + snapshot.size() + " Android item(s)");
                mobilegonc.Session goSession = Mobilegonc.startP2PShareSource(source, password, useUdp, bridgeCallback(callback, session, false));
                session.attach(goSession);
            } catch (Throwable error) {
                if (!session.isStopped()) {
                    callback.onError(error);
                }
            }
        }, "gonc-share");
        worker.start();
        return session;
    }

    @Override
    public Session startP2PReceive(Context context, String password, boolean useUdp, EventCallback callback) {
        BridgeSession session = new BridgeSession();
        Thread worker = new Thread(() -> {
            try {
                callback.onEvent("info", "Starting gonc mobile receive engine");
                mobilegonc.Session goSession = Mobilegonc.startP2PReceive(password, useUdp, bridgeCallback(callback, session, false));
                session.attach(goSession);
            } catch (Throwable error) {
                if (!session.isStopped()) {
                    callback.onError(error);
                }
            }
        }, "gonc-receive");
        worker.start();
        return session;
    }

    @Override
    public Session startP2PLinkAgent(Context context, String password, boolean useUdp, String extraArgs, EventCallback callback) {
        BridgeSession session = new BridgeSession();
        Thread worker = new Thread(() -> {
            try {
                callback.onEvent("info", "Starting gonc mobile linkagent (VPN server) engine");
                mobilegonc.Session goSession = Mobilegonc.startP2PLinkAgent(password, useUdp, extraArgs == null ? "" : extraArgs, bridgeCallback(callback, session, true));
                session.attach(goSession);
            } catch (Throwable error) {
                if (!session.isStopped()) {
                    callback.onError(error);
                }
            }
        }, "gonc-linkagent");
        worker.start();
        return session;
    }

    private static Callback bridgeCallback(EventCallback callback, BridgeSession session, boolean reportExitErrors) {
        return new Callback() {
            private volatile String lastMessage = "";

            @Override
            public void event(String level, String message) {
                callback.onEvent(level, message);
                if (message != null && !message.trim().isEmpty()) {
                    lastMessage = message.trim();
                }
            }

            @Override
            public void p2PReport(String topic, String status, String network, String mode, String peer, long timestamp, long pid) {
                callback.onP2PReport(topic, status, network, mode, peer, timestamp, pid);
            }

            @Override
            public void ready(String endpoint) {
                callback.onReady(endpoint);
            }

            @Override
            public void stopped(long exitCode) {
                // A non-zero exit that the user did not trigger is a real failure
                // (e.g. gonc rejected bad extra args). Surface it as an error rather
                // than a silent stop, when the caller opted in (the VPN server).
                if (reportExitErrors && exitCode != 0 && !session.isStopped()) {
                    callback.onEvent("error", "gonc exited with code " + exitCode);
                    String detail = lastMessage;
                    if (detail != null && detail.length() > 300) {
                        detail = detail.substring(0, 300) + "...";
                    }
                    String message = "exit code " + exitCode + (detail == null || detail.isEmpty() ? "" : ": " + detail);
                    callback.onError(new RuntimeException(message));
                    return;
                }
                callback.onEvent(exitCode == 0 ? "info" : "error", "gonc stopped with code " + exitCode);
                callback.onStopped();
            }

            @Override
            public void error(String message) {
                callback.onError(new RuntimeException(message));
            }
        };
    }

    private static final class BridgeSession implements Session {
        private volatile boolean stopped;
        private mobilegonc.Session goSession;
        private AndroidFileSource source;
        private List<ShareItem> pendingShareItems;

        @Override
        public synchronized void stop() {
            stopped = true;
            if (goSession != null) {
                goSession.stop();
            }
            if (source != null) {
                source.closeAll();
            }
        }

        @Override
        public synchronized void updateShareItems(List<ShareItem> items) {
            List<ShareItem> snapshot = new ArrayList<>(items);
            if (source != null) {
                source.updateItems(snapshot);
            } else {
                pendingShareItems = snapshot;
            }
        }

        synchronized void attach(mobilegonc.Session goSession) {
            this.goSession = goSession;
            if (stopped && goSession != null) {
                goSession.stop();
            }
        }

        synchronized void attachSource(AndroidFileSource source) {
            this.source = source;
            if (pendingShareItems != null && source != null) {
                source.updateItems(pendingShareItems);
                pendingShareItems = null;
            }
            if (stopped && source != null) {
                source.closeAll();
            }
        }

        boolean isStopped() {
            return stopped;
        }
    }
}
