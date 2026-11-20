package cn.threatexpert.gonc;

/** One connected path owns the receive state until its own disconnect report. */
final class ReceiveReportState {
    private String connectedTopic;

    void reset() { connectedTopic = null; }

    boolean accept(String topic, String status) {
        String key = topic == null ? "" : topic;
        if (connectedTopic != null && !connectedTopic.equals(key)) return false;
        if ("connected".equals(status)) connectedTopic = key;
        else if ("disconnected".equals(status) || "stopped".equals(status) || "finished".equals(status)) connectedTopic = null;
        return true;
    }

    static boolean isWaitTimeout(String status) {
        return status != null && "error:hello-timeout".equalsIgnoreCase(status.trim());
    }
}
