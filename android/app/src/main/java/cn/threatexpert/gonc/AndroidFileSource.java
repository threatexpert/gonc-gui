package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.Context;
import android.database.Cursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import android.provider.OpenableColumns;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.FileInputStream;
import java.io.InputStream;
import java.nio.channels.FileChannel;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicLong;

final class AndroidFileSource implements mobilegonc.AndroidFileSource {
    private final ContentResolver resolver;
    private final Object rootsLock = new Object();
    private final List<Node> roots = new ArrayList<>();
    private final Map<Long, OpenHandle> streams = new HashMap<>();
    private final AtomicLong nextHandle = new AtomicLong(1);

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
            roots.addAll(updated);
        }
    }

    @Override
    public String description() {
        return rootSnapshot().size() + " Android item(s)";
    }

    @Override
    public String stat(String name) {
        try {
            Node node = resolve(name);
            return node == null ? "" : node.toJson().toString();
        } catch (Exception ignored) {
            return "";
        }
    }

    @Override
    public String readDir(String name) {
        try {
            Node node = resolve(name);
            if (node == null || !node.directory) {
                return "";
            }
            JSONArray array = new JSONArray();
            for (Node child : node.children()) {
                array.put(child.toJson());
            }
            return array.toString();
        } catch (Exception ignored) {
            return "";
        }
    }

    @Override
    public long open(String name) {
        try {
            Node node = resolve(name);
            if (node == null || node.directory) {
                return 0;
            }
            OpenHandle openHandle = OpenHandle.open(resolver, node.uri);
            if (openHandle == null) {
                return 0;
            }
            long handle = nextHandle.getAndIncrement();
            synchronized (streams) {
                streams.put(handle, openHandle);
            }
            return handle;
        } catch (Exception ignored) {
            return 0;
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
            handles = new ArrayList<>(streams.keySet());
        }
        for (Long handle : handles) {
            close(handle);
        }
    }

    private Node resolve(String name) throws Exception {
        String clean = cleanPath(name);
        if ("/".equals(clean)) {
            return new Node("/", null, true, 0, DocumentsContract.Document.MIME_TYPE_DIR, 0, false) {
                @Override
                List<Node> children() {
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
            Node next = null;
            for (Node child : current.children()) {
                if (child.name.equals(parts[i])) {
                    next = child;
                    break;
                }
            }
            current = next;
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

        Node(String name, Uri uri, boolean directory, long size, String mimeType, long lastModifiedMillis, boolean treeUri) {
            this.name = name;
            this.uri = uri;
            this.directory = directory;
            this.size = size;
            this.mimeType = mimeType == null ? "application/octet-stream" : mimeType;
            this.lastModifiedMillis = lastModifiedMillis;
            this.treeUri = treeUri;
        }

        JSONObject toJson() throws Exception {
            JSONObject object = new JSONObject();
            object.put("name", name);
            object.put("isDir", directory);
            object.put("size", directory ? 0 : size);
            object.put("modTimeMillis", lastModifiedMillis);
            return object;
        }

        List<Node> children() throws Exception {
            if (!directory) {
                return new ArrayList<>();
            }
            return loadChildren(this);
        }
    }

    private List<Node> loadChildren(Node parent) throws Exception {
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
            return out;
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
                out.add(new Node(uniqueName(safeName(displayName), seen), childUri, isDir, size, mimeType, lastModified, false));
            }
        } finally {
            cursor.close();
        }
        return out;
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
        seen.put(base, next);
        int dot = base.lastIndexOf('.');
        String stem = dot > 0 ? base.substring(0, dot) : base;
        String ext = dot > 0 ? base.substring(dot) : "";
        return stem + "-" + next + ext;
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
