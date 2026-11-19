package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.Context;
import android.database.Cursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import android.provider.OpenableColumns;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileNotFoundException;
import java.io.InputStream;
import java.nio.channels.FileChannel;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicLong;

final class AndroidFileSource implements mobilegonc.AndroidFileSource {
    private static final String TAG = "GoncShareSource";
    private final ContentResolver resolver;
    private final Object rootsLock = new Object();
    private final List<Node> roots = new ArrayList<>();
    private final Map<Long, OpenHandle> streams = new HashMap<>();
    private final AtomicLong nextHandle = new AtomicLong(1);
    private final AtomicLong directoryQueries = new AtomicLong();
    private volatile boolean closed;

    AndroidFileSource(Context context, List<ShareItem> items) {
        this.resolver = context.getApplicationContext().getContentResolver();
        updateItems(items);
    }

    void updateItems(List<ShareItem> items) {
        List<Node> updated = new ArrayList<>();
        Map<String, Integer> seen = new LinkedHashMap<>();
        for (ShareItem item : items) {
            String name = uniqueName(safeName(item.displayName()), seen);
            updated.add(new Node(name, item.uri(), item.isDirectory(), Math.max(-1, item.size()), item.mimeType(), item.lastModifiedMillis(), item.isTreeUri()));
        }
        synchronized (rootsLock) {
            roots.clear();
            if (!closed) roots.addAll(updated);
        }
    }

    @Override
    public String description() {
        return rootSnapshot().size() + " Android item(s)";
    }

    @Override
    public String stat(String name) {
        long started = System.nanoTime();
        try {
            for (int attempt = 0; attempt < 2; attempt++) {
                try {
                    Node node = resolve(name, attempt > 0);
                    return node == null ? "" : freshMetadata(node).toJson().toString();
                } catch (Exception error) {
                    if (attempt == 1) return "";
                }
            }
            return "";
        } finally {
            logSlow("stat", started);
        }
    }

    @Override
    public String readDir(String name) {
        for (int attempt = 0; attempt < 2; attempt++) {
            try {
                Node node = resolve(name, attempt > 0);
                if (node == null || !node.directory) return "";
                JSONArray array = new JSONArray();
                for (Node child : node.refreshChildren()) array.put(child.toJson());
                return array.toString();
            } catch (Exception error) {
                if (attempt == 1) return "";
            }
        }
        return "";
    }

    @Override
    public long open(String name) {
        long started = System.nanoTime();
        try {
            for (int attempt = 0; attempt < 2; attempt++) {
                try {
                    Node node = resolve(name, attempt > 0);
                    if (node == null || node.directory) return 0;
                    // Check the one document, including rename, without enumerating its siblings.
                    freshMetadata(node);
                    long openStarted = System.nanoTime();
                    OpenHandle openHandle;
                    try {
                        openHandle = OpenHandle.open(resolver, node.uri);
                    } finally {
                        logSlow("open file", openStarted);
                    }
                    if (openHandle == null) throw new FileNotFoundException(name);
                    synchronized (streams) {
                        if (closed) {
                            openHandle.close();
                            return 0;
                        }
                        long handle = nextHandle.getAndIncrement();
                        streams.put(handle, openHandle);
                        return handle;
                    }
                } catch (Exception error) {
                    if (attempt == 1) return 0;
                }
            }
            return 0;
        } finally {
            logSlow("open", started);
        }
    }

    @Override
    public byte[] read(long handle, long maxBytes) {
        OpenHandle openHandle;
        synchronized (streams) {
            openHandle = streams.get(handle);
        }
        if (openHandle == null || maxBytes <= 0) {
            return new byte[0];
        }
        int size = (int) Math.min(maxBytes, 128 * 1024L);
        byte[] buffer = new byte[size];
        try {
            int n = openHandle.read(buffer);
            if (n <= 0) {
                return new byte[0];
            }
            if (n == buffer.length) {
                return buffer;
            }
            byte[] out = new byte[n];
            System.arraycopy(buffer, 0, out, 0, n);
            return out;
        } catch (Exception ignored) {
            return new byte[0];
        }
    }

    @Override
    public long seek(long handle, long offset, long whence) {
        OpenHandle openHandle;
        synchronized (streams) {
            openHandle = streams.get(handle);
        }
        if (openHandle == null) {
            return -1;
        }
        try {
            return openHandle.seek(offset, whence);
        } catch (Exception ignored) {
            return -1;
        }
    }

    @Override
    public void close(long handle) {
        OpenHandle openHandle;
        synchronized (streams) {
            openHandle = streams.remove(handle);
        }
        if (openHandle != null) {
            openHandle.close();
        }
    }

    void closeAll() {
        List<Long> handles;
        synchronized (streams) {
            closed = true;
            handles = new ArrayList<>(streams.keySet());
        }
        synchronized (rootsLock) {
            roots.clear(); // Releases all directory indexes with the sharing session.
        }
        for (Long handle : handles) {
            close(handle);
        }
    }

    private Node resolve(String name, boolean refresh) throws Exception {
        if (closed) return null;
        String clean = cleanPath(name);
        if ("/".equals(clean)) {
            return new Node("/", null, true, 0, DocumentsContract.Document.MIME_TYPE_DIR, 0, false) {
                @Override
                List<Node> refreshChildren() {
                    return rootSnapshot();
                }
            };
        }
        String[] parts = clean.substring(1).split("/");
        Node current = null;
        for (Node root : rootSnapshot()) {
            if (root.name.equals(parts[0])) {
                current = root;
                break;
            }
        }
        if (current == null) {
            return null;
        }
        for (int i = 1; i < parts.length; i++) {
            if (!current.directory) {
                return null;
            }
            current = current.index.find(parts[i], refresh);
            if (current == null) {
                return null;
            }
        }
        return current;
    }

    private List<Node> rootSnapshot() {
        synchronized (rootsLock) {
            return new ArrayList<>(roots);
        }
    }

    private String cleanPath(String value) {
        if (value == null || value.trim().isEmpty() || ".".equals(value.trim())) {
            return "/";
        }
        String clean = value.trim();
        if (!clean.startsWith("/")) {
            clean = "/" + clean;
        }
        while (clean.contains("//")) {
            clean = clean.replace("//", "/");
        }
        while (clean.endsWith("/") && clean.length() > 1) {
            clean = clean.substring(0, clean.length() - 1);
        }
        if (clean.contains("/../") || clean.endsWith("/..") || clean.contains("/./")) {
            return "/";
        }
        return clean;
    }

    private class Node {
        final String name;
        final Uri uri;
        final boolean directory;
        final long size;
        final String mimeType;
        final long lastModifiedMillis;
        final boolean treeUri;
        final String documentName;
        final ShareDirectoryIndex<Node> index = new ShareDirectoryIndex<>(
                () -> loadChildren(this), node -> node.name);

        Node(String name, Uri uri, boolean directory, long size, String mimeType, long lastModifiedMillis, boolean treeUri) {
            this(name, uri, directory, size, mimeType, lastModifiedMillis, treeUri, null);
        }

        Node(String name, Uri uri, boolean directory, long size, String mimeType, long lastModifiedMillis, boolean treeUri, String documentName) {
            this.name = name;
            this.uri = uri;
            this.directory = directory;
            this.size = size;
            this.mimeType = mimeType == null ? "application/octet-stream" : mimeType;
            this.lastModifiedMillis = lastModifiedMillis;
            this.treeUri = treeUri;
            this.documentName = documentName;
        }

        JSONObject toJson() throws Exception {
            JSONObject object = new JSONObject();
            object.put("name", name);
            object.put("isDir", directory);
            object.put("size", directory ? 0 : size);
            object.put("modTimeMillis", lastModifiedMillis);
            // App-owned files have Unix metadata. SAF documents have no mode.
            if (uri != null && "file".equals(uri.getScheme()) && uri.getPath() != null) {
                try {
                    object.put("mode", android.system.Os.stat(uri.getPath()).st_mode & 0777);
                } catch (android.system.ErrnoException ignored) {
                    // Keep the entry usable if optional metadata is unavailable.
                }
            }
            return object;
        }

        List<Node> refreshChildren() throws Exception {
            if (!directory) {
                return new ArrayList<>();
            }
            return index.refresh();
        }
    }

    private List<Node> loadChildren(Node parent) throws Exception {
        long started = System.nanoTime();
        List<Node> out = new ArrayList<>();
        if (parent.uri == null) {
            return out;
        }
        String parentDocId;
        if (parent.treeUri) {
            parentDocId = DocumentsContract.getTreeDocumentId(parent.uri);
        } else {
            parentDocId = DocumentsContract.getDocumentId(parent.uri);
        }
        Uri childrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(parent.uri, parentDocId);
        Cursor cursor = resolver.query(childrenUri, new String[]{
                DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                DocumentsContract.Document.COLUMN_MIME_TYPE,
                DocumentsContract.Document.COLUMN_SIZE,
                DocumentsContract.Document.COLUMN_LAST_MODIFIED
        }, null, null, null);
        if (cursor == null) {
            throw new FileNotFoundException("Cannot query shared directory");
        }
        Map<String, Integer> seen = new LinkedHashMap<>();
        try {
            while (cursor.moveToNext()) {
                String docId = cursor.getString(0);
                String displayName = cursor.getString(1);
                String mimeType = cursor.getString(2);
                long size = cursor.isNull(3) ? -1 : cursor.getLong(3);
                long lastModified = cursor.isNull(4) ? 0 : cursor.getLong(4);
                boolean isDir = DocumentsContract.Document.MIME_TYPE_DIR.equals(mimeType);
                Uri childUri = DocumentsContract.buildDocumentUriUsingTree(parent.uri, docId);
                out.add(new Node(uniqueName(safeName(displayName), seen), childUri, isDir, size, mimeType, lastModified, false, displayName));
            }
        } finally {
            cursor.close();
            Log.d(TAG, "directory query #" + directoryQueries.incrementAndGet()
                    + " entries=" + out.size() + " elapsedMs=" + elapsedMs(started));
        }
        return out;
    }

    private Node freshMetadata(Node node) throws Exception {
        long started = System.nanoTime();
        try {
            return queryMetadata(node);
        } finally {
            logSlow("metadata query", started);
        }
    }

    private Node queryMetadata(Node node) throws Exception {
        if (node.uri == null) return node;
        if ("file".equalsIgnoreCase(node.uri.getScheme())) {
            File file = new File(node.uri.getPath());
            if (!file.exists()) throw new FileNotFoundException(node.name);
            return new Node(node.name, node.uri, file.isDirectory(), file.length(),
                    node.mimeType, file.lastModified(), false, node.documentName);
        }
        Uri queryUri = node.treeUri ? DocumentsContract.buildDocumentUriUsingTree(
                node.uri, DocumentsContract.getTreeDocumentId(node.uri)) : node.uri;
        // Query only this document. Optional columns vary across Documents/Media providers.
        try (Cursor cursor = resolver.query(queryUri, null, null, null, null)) {
            if (cursor == null || !cursor.moveToFirst()) throw new FileNotFoundException(node.name);
            int nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME);
            if (node.documentName != null && nameColumn >= 0
                    && !node.documentName.equals(cursor.getString(nameColumn))) {
                throw new FileNotFoundException("Shared document was renamed");
            }
            int sizeColumn = cursor.getColumnIndex(OpenableColumns.SIZE);
            long size = sizeColumn < 0 || cursor.isNull(sizeColumn) ? -1 : cursor.getLong(sizeColumn);
            int modifiedColumn = cursor.getColumnIndex(DocumentsContract.Document.COLUMN_LAST_MODIFIED);
            long modified = 0;
            if (modifiedColumn >= 0 && !cursor.isNull(modifiedColumn)) {
                modified = cursor.getLong(modifiedColumn);
            } else {
                int secondsColumn = cursor.getColumnIndex("date_modified");
                if (secondsColumn >= 0 && !cursor.isNull(secondsColumn)) modified = cursor.getLong(secondsColumn) * 1000L;
            }
            return new Node(node.name, node.uri, node.directory, size, node.mimeType,
                    modified, node.treeUri, node.documentName);
        }
    }

    private static long elapsedMs(long started) {
        return (System.nanoTime() - started) / 1_000_000L;
    }

    private static void logSlow(String operation, long started) {
        long elapsed = elapsedMs(started);
        if (elapsed >= 100) Log.d(TAG, operation + " elapsedMs=" + elapsed);
    }

    private static String safeName(String value) {
        String name = value == null ? "" : value.replaceAll("[\\\\/:*?\"<>|\\p{Cntrl}]", "_").trim();
        if (name.isEmpty() || ".".equals(name) || "..".equals(name)) {
            return "shared-file";
        }
        return name;
    }

    private static String uniqueName(String base, Map<String, Integer> seen) {
        Integer count = seen.get(base);
        if (count == null) {
            seen.put(base, 1);
            return base;
        }
        int next = count + 1;
        int dot = base.lastIndexOf('.');
        String stem = dot > 0 ? base.substring(0, dot) : base;
        String ext = dot > 0 ? base.substring(dot) : "";
        String candidate = stem + "-" + next + ext;
        while (seen.containsKey(candidate)) candidate = stem + "-" + (++next) + ext;
        seen.put(base, next);
        seen.put(candidate, 1);
        return candidate;
    }

    private static final class OpenHandle {
        private final InputStream input;
        private final FileChannel channel;
        private final ParcelFileDescriptor descriptor;

        private OpenHandle(InputStream input, FileChannel channel, ParcelFileDescriptor descriptor) {
            this.input = input;
            this.channel = channel;
            this.descriptor = descriptor;
        }

        static OpenHandle open(ContentResolver resolver, Uri uri) throws Exception {
            if ("file".equalsIgnoreCase(uri.getScheme())) {
                FileInputStream input = new FileInputStream(new File(uri.getPath()));
                return new OpenHandle(input, input.getChannel(), null);
            }
            ParcelFileDescriptor descriptor = resolver.openFileDescriptor(uri, "r");
            if (descriptor != null) {
                FileInputStream input = new FileInputStream(descriptor.getFileDescriptor());
                return new OpenHandle(input, input.getChannel(), descriptor);
            }
            InputStream input = resolver.openInputStream(uri);
            return input == null ? null : new OpenHandle(input, null, null);
        }

        int read(byte[] buffer) throws Exception {
            return input.read(buffer);
        }

        long seek(long offset, long whence) throws Exception {
            if (channel == null) {
                return -1;
            }
            long target;
            if (whence == 0) {
                target = offset;
            } else if (whence == 1) {
                target = channel.position() + offset;
            } else if (whence == 2) {
                target = channel.size() + offset;
            } else {
                return -1;
            }
            if (target < 0) {
                return -1;
            }
            channel.position(target);
            return channel.position();
        }

        void close() {
            try {
                input.close();
            } catch (Exception ignored) {
            }
            if (descriptor != null) {
                try {
                    descriptor.close();
                } catch (Exception ignored) {
                }
            }
        }
    }
}
