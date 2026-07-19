package cn.threatexpert.gonc;

final class ReceivedFileMatcher {
    private ReceivedFileMatcher() {
    }

    static boolean isAvailable(boolean exists, boolean readable, boolean directory, long actualSize, long expectedSize) {
        return exists && readable && !directory && actualSize >= 0 && expectedSize >= 0 && actualSize == expectedSize;
    }
}
