package cn.threatexpert.gonc;

import android.net.Uri;

import java.io.File;

final class ShareItem {
    private final Uri uri;
    private final String displayName;
    private final long size;
    private final String mimeType;
    private final boolean directory;
    private final boolean treeUri;
    private final long lastModifiedMillis;
    private File cachedFile;

    ShareItem(Uri uri, String displayName, long size, String mimeType) {
        this(uri, displayName, size, mimeType, false, false, 0);
    }

    ShareItem(Uri uri, String displayName, long size, String mimeType, boolean directory, boolean treeUri, long lastModifiedMillis) {
        this.uri = uri;
        this.displayName = displayName == null || displayName.trim().isEmpty() ? "shared-file" : displayName;
        this.size = size;
        this.mimeType = mimeType == null ? "application/octet-stream" : mimeType;
        this.directory = directory;
        this.treeUri = treeUri;
        this.lastModifiedMillis = lastModifiedMillis;
    }

    Uri uri() {
        return uri;
    }

    String displayName() {
        return displayName;
    }

    long size() {
        return size;
    }

    String mimeType() {
        return mimeType;
    }

    boolean isDirectory() {
        return directory;
    }

    boolean isTreeUri() {
        return treeUri;
    }

    long lastModifiedMillis() {
        return lastModifiedMillis;
    }

    File cachedFile() {
        return cachedFile;
    }

    void setCachedFile(File cachedFile) {
        this.cachedFile = cachedFile;
    }
}
