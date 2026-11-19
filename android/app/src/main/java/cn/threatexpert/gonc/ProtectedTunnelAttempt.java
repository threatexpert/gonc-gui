package cn.threatexpert.gonc;

import java.util.Locale;

/** One attempt's event mailbox; never polls physical networking. */
final class ProtectedTunnelAttempt {
    private boolean active = true, connected, lost;
    private String topic = "", endpoint = "";
    private long revision;

    synchronized boolean active() { return active; }
    synchronized boolean lost() { return lost; }
    synchronized void retire() { active = false; notifyAll(); }

    synchronized boolean report(String nextTopic, String status) {
        if (!active || lost) return false;
        String s = status == null ? "" : status.trim().toLowerCase(Locale.ROOT);
        String t = nextTopic == null ? "" : nextTopic;
        if (connected && !topic.isEmpty() && !t.isEmpty() && !topic.equals(t)) return false;
        if (s.equals("connected")) { connected = true; topic = t; return true; }
        if (connected && (s.equals("connecting") || s.equals("negotiating") || s.equals("reconnecting") || s.equals("waiting") || s.equals("idle") || s.equals("ready") || s.equals("discovering") || s.equals("wait"))) return false;
        if (s.equals("disconnected") || s.equals("disconnect") || s.equals("closed") || s.equals("stopped") || s.contains("fail") || s.contains("error") || s.contains("lost") || s.contains("timeout")) fail();
        return true;
    }

    synchronized boolean ready(String next) {
        if (!active || lost || !connected || next == null || !next.startsWith("socks5://")) return false;
        endpoint = next; revision++; notifyAll(); return true;
    }

    synchronized void fail() { if (active) { lost = true; endpoint = ""; notifyAll(); } }
    synchronized long revision() { return revision; }
    synchronized String endpoint() { return active && !lost ? endpoint : ""; }

    synchronized void awaitChange(long seen) throws InterruptedException {
        while (active && !lost && revision == seen) wait();
    }
}
