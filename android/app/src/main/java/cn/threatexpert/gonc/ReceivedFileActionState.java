package cn.threatexpert.gonc;

final class ReceivedFileActionState {
    private ReceivedFileActionState() {
    }

    static boolean markerVisible(boolean targetAvailable) {
        return targetAvailable;
    }

    static boolean actionsEnabled(boolean targetAvailable, boolean downloadActive) {
        return targetAvailable && !downloadActive;
    }
}
