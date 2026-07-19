package cn.threatexpert.gonc;

final class ReceivedFileActionState {
    private ReceivedFileActionState() {
    }

    static boolean markerVisible(boolean targetAvailable) {
        return targetAvailable;
    }

    static boolean actionsEnabled(
            boolean targetAvailable,
            boolean downloadActive,
            boolean completionRefreshPending) {
        return targetAvailable && !downloadActive && !completionRefreshPending;
    }

    static boolean ownsCompletionRefresh(long pendingToken, long finishedToken) {
        return pendingToken != 0L && pendingToken == finishedToken;
    }
}
