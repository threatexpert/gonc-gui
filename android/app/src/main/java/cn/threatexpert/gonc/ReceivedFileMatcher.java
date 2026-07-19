package cn.threatexpert.gonc;

final class ReceivedFileMatcher {
    private ReceivedFileMatcher() {
    }

    static boolean isAvailable(boolean exists, boolean readable, boolean directory, long actualSize, long expectedSize) {
        return exists && readable && !directory && actualSize >= 0 && expectedSize >= 0 && actualSize == expectedSize;
    }

    static long reportedSize(boolean known, long size) {
        return known ? size : -1;
    }

    static int firstAvailableIndex(boolean[] readable, boolean[] directory, long[] sizes, long expectedSize) {
        if (readable == null || directory == null || sizes == null
                || readable.length != directory.length || readable.length != sizes.length) {
            return -1;
        }
        for (int i = 0; i < sizes.length; i++) {
            if (isAvailable(true, readable[i], directory[i], sizes[i], expectedSize)) {
                return i;
            }
        }
        return -1;
    }
}
