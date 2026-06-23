package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.ContentValues;
import android.content.Context;
import android.content.SharedPreferences;
import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.DocumentsContract;
import android.provider.MediaStore;
import android.provider.OpenableColumns;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

final class HttpReceiver {
    private static final String DEFAULT_SAVE_FOLDER = "Gonc";

    interface ListCallback {
        void onList(List<RemoteFile> files, int fileCount, int dirCount, long totalBytes, boolean missing);

        void onError(Throwable error);
    }

    interface Callback {
        void onProgress(int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current);

        void onComplete(int totalFiles, long totalBytes, long networkBytes, int skippedFiles, int resumedFiles);

        void onError(Throwable error);
    }

    static Session startList(String serverUrl, ListCallback callback) {
        return startList(serverUrl, "", callback);
    }

    static Session startList(String serverUrl, String subPath, ListCallback callback) {
        List<String> paths = new ArrayList<>();
        paths.add(subPath);
        return startList(serverUrl, paths, callback);
    }

    static Session startList(String serverUrl, List<String> subPaths, ListCallback callback) {
        Session session = new Session();
        Thread worker = new Thread(() -> {
            try {
                ListResult result = listMany(serverUrl, subPaths, session);
                List<RemoteFile> files = dropRootDirectories(result.files);
                int fileCount = 0;
                int dirCount = 0;
                long totalBytes = 0;
                for (RemoteFile file : files) {
                    if (file.isDir) {
                        dirCount++;
                    } else {
                        fileCount++;
                        totalBytes += Math.max(0, file.size);
                    }
                }
                if (callback != null && !session.isStopped()) {
                    callback.onList(files, fileCount, dirCount, totalBytes, result.missing);
                }
            } catch (Throwable error) {
                if (callback != null && !session.isStopped()) {
                    callback.onError(error);
                }
            }
        }, "gonc-http-list");
        worker.start();
        return session;
    }

    static Session start(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, Callback callback) {
        Session session = new Session();
        Thread worker = new Thread(() -> run(context.getApplicationContext(), serverUrl, saveTreeUri, requestedFiles, resume, session, callback), "gonc-http-receive");
        worker.start();
        return session;
    }

    private static void run(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, Session session, Callback callback) {
        try {
            List<RemoteFile> files = requestedFiles == null ? dropRootDirectories(list(serverUrl, "", session).files) : new ArrayList<>(requestedFiles);
            long totalBytes = 0;
            int totalFiles = 0;
            for (RemoteFile file : files) {
                if (!file.isDir) {
                    totalFiles++;
                    totalBytes += Math.max(0, file.size);
                }
            }

            int doneFiles = 0;
            long doneBytes = 0;
            long networkDoneBytes = 0;
            int skippedFiles = 0;
            int resumedFiles = 0;
            ProgressClock clock = new ProgressClock();
            TargetResolver targetResolver = new TargetResolver(context, saveTreeUri);
            if (callback != null) {
                callback.onProgress(0, totalFiles, 0, totalBytes, 0, 0, "");
            }
            for (RemoteFile file : files) {
                if (session.isStopped()) {
                    return;
                }
                if (file.isDir) {
                    targetResolver.ensureDirectory(file.path);
                    continue;
                }
                DownloadResult result = downloadOne(context, serverUrl, file, targetResolver, resume, session, callback, clock, doneFiles, totalFiles, doneBytes, networkDoneBytes, totalBytes);
                doneFiles++;
                doneBytes += result.doneBytes;
                networkDoneBytes += result.networkBytes;
                if (result.skipped) {
                    skippedFiles++;
                }
                if (result.resumed) {
                    resumedFiles++;
                }
                if (callback != null) {
                    callback.onProgress(doneFiles, totalFiles, doneBytes, totalBytes, networkDoneBytes, clock.speedFor(networkDoneBytes), file.path);
                }
            }
            if (callback != null && !session.isStopped()) {
                callback.onComplete(totalFiles, totalBytes, networkDoneBytes, skippedFiles, resumedFiles);
            }
        } catch (Throwable error) {
            if (callback != null && !session.isStopped()) {
                callback.onError(error);
            }
        }
    }

    private static ListResult listMany(String serverUrl, List<String> subPaths, Session session) throws Exception {
        Map<String, RemoteFile> merged = new LinkedHashMap<>();
        if (subPaths == null || subPaths.isEmpty()) {
            subPaths = new ArrayList<>();
            subPaths.add("");
        }
        int requested = 0;
        int missing = 0;
        for (String subPath : subPaths) {
            if (session.isStopped()) {
                break;
            }
            requested++;
            ListResult result = list(serverUrl, subPath, session);
            if (result.missing) {
                missing++;
                continue;
            }
            for (RemoteFile file : result.files) {
                String key = normalizePath(file.path);
                if (!merged.containsKey(key)) {
                    merged.put(key, file);
                }
            }
        }
        return new ListResult(new ArrayList<>(merged.values()), requested > 0 && missing == requested);
    }

    private static ListResult list(String serverUrl, String subPath, Session session) throws Exception {
        HttpURLConnection conn = open(resolveUrl(serverUrl, subPath));
        conn.setRequestProperty("Accept", "application/json");
        if (conn.getResponseCode() == HttpURLConnection.HTTP_NOT_FOUND) {
            return new ListResult(new ArrayList<>(), true);
        }
        if (conn.getResponseCode() != HttpURLConnection.HTTP_OK) {
            throw new IllegalStateException("Remote list failed: HTTP " + conn.getResponseCode());
        }
        List<RemoteFile> files = new ArrayList<>();
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(conn.getInputStream(), StandardCharsets.UTF_8))) {
            String line;
            while ((line = reader.readLine()) != null) {
                if (session.isStopped()) {
                    return new ListResult(files, false);
                }
                line = line.trim();
                if (line.isEmpty()) {
                    continue;
                }
                JSONObject json = new JSONObject(line);
                RemoteFile file = new RemoteFile();
                file.name = json.optString("name");
                file.isDir = json.optBoolean("is_dir");
                file.size = json.optLong("size");
                file.path = json.optString("path");
                file.modifiedMs = parseModTime(json.optString("mod_time"));
                if (file.path == null || file.path.trim().isEmpty()) {
                    file.path = file.name;
                }
                files.add(file);
            }
        } finally {
            conn.disconnect();
        }
        return new ListResult(files, false);
    }

    /** Parse the server's RFC3339 {@code mod_time} into epoch millis; 0 when absent/zero/unparseable. */
    private static long parseModTime(String value) {
        if (value == null) {
            return 0;
        }
        String trimmed = value.trim();
        if (trimmed.isEmpty()) {
            return 0;
        }
        try {
            long ms = java.time.OffsetDateTime.parse(trimmed).toInstant().toEpochMilli();
            return ms > 0 ? ms : 0; // drop Go's zero time (year 0001) / pre-epoch
        } catch (RuntimeException error) {
            return 0;
        }
    }

    private static String normalizePath(String path) {
        String clean = path == null ? "" : path.trim().replace('\\', '/');
        while (clean.startsWith("/")) {
            clean = clean.substring(1);
        }
        while (clean.endsWith("/") && clean.length() > 1) {
            clean = clean.substring(0, clean.length() - 1);
        }
        return ".".equals(clean) ? "" : clean;
    }

    private static DownloadResult downloadOne(Context context, String serverUrl, RemoteFile file, TargetResolver targetResolver, boolean resume, Session session, Callback callback, ProgressClock clock, int doneFiles, int totalFiles, long baseDoneBytes, long baseNetworkBytes, long totalBytes) throws Exception {
        DocumentInfo target = targetResolver.ensureFile(file.path, file.name, !resume);
        long offset = resume ? Math.max(0, target.size) : 0;
        if (resume && offset >= file.size && file.size >= 0) {
            return new DownloadResult(file.size, 0, true, false);
        }

        HttpURLConnection conn = open(resolveUrl(serverUrl, file.path));
        session.attach(conn);
        if (offset > 0) {
            conn.setRequestProperty("Range", "bytes=" + offset + "-");
        }
        try {
            int status = conn.getResponseCode();
            boolean resumed = offset > 0 && status == HttpURLConnection.HTTP_PARTIAL;
            if (offset > 0 && status == HttpURLConnection.HTTP_OK) {
                offset = 0;
            } else if (status != HttpURLConnection.HTTP_OK && status != HttpURLConnection.HTTP_PARTIAL) {
                throw new IllegalStateException("Download failed for " + file.path + ": HTTP " + status);
            }

            long copied = offset;
            long initialCopied = offset;
            if (callback != null && offset > 0) {
                callback.onProgress(doneFiles, totalFiles, baseDoneBytes + offset, totalBytes, baseNetworkBytes, clock.speedFor(baseNetworkBytes), file.path);
            }
            ContentResolver resolver = context.getContentResolver();
            try (InputStream input = conn.getInputStream(); OutputStream output = resolver.openOutputStream(target.uri, offset > 0 ? "wa" : "wt")) {
                if (output == null) {
                    throw new IllegalStateException("Cannot open destination: " + file.path);
                }
                byte[] buffer = new byte[128 * 1024];
                int n;
                while ((n = input.read(buffer)) >= 0) {
                    if (session.isStopped()) {
                        return new DownloadResult(copied, Math.max(0, copied - initialCopied), false, resumed);
                    }
                    output.write(buffer, 0, n);
                    copied += n;
                    long networkCopied = Math.max(0, copied - initialCopied);
                    if (callback != null && clock.shouldEmit(baseNetworkBytes + networkCopied)) {
                        callback.onProgress(doneFiles, totalFiles, baseDoneBytes + copied, totalBytes, baseNetworkBytes + networkCopied, clock.speedFor(baseNetworkBytes + networkCopied), file.path);
                    }
                }
            }
            return new DownloadResult(copied, Math.max(0, copied - initialCopied), false, resumed);
        } finally {
            session.detach(conn);
        }
    }

    private static HttpURLConnection open(String url) throws Exception {
        HttpURLConnection conn = (HttpURLConnection) new URL(url).openConnection();
        conn.setConnectTimeout(10000);
        conn.setReadTimeout(30000);
        return conn;
    }

    private static DocumentInfo ensureFile(Context context, Uri treeUri, String remotePath, String fallbackName, boolean replaceExisting) throws Exception {
        if (treeUri == null) {
            return ensurePublicDownloadFile(context, remotePath, fallbackName, replaceExisting);
        }
        String[] parts = pathParts(remotePath);
        Uri parent = rootDocumentUri(treeUri);
        for (int i = 0; i < parts.length - 1; i++) {
            parent = findOrCreate(context, treeUri, parent, parts[i], DocumentsContract.Document.MIME_TYPE_DIR);
        }
        String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
        DocumentInfo existing = findChild(context, treeUri, parent, name);
        if (existing != null && replaceExisting) {
            DocumentsContract.deleteDocument(context.getContentResolver(), existing.uri);
            existing = null;
        }
        if (existing != null) {
            return existing;
        }
        Uri created = DocumentsContract.createDocument(context.getContentResolver(), parent, "application/octet-stream", name);
        if (created == null) {
            throw new IllegalStateException("Cannot create " + name);
        }
        return new DocumentInfo(created, 0);
    }

    private static Uri ensureDirectory(Context context, Uri treeUri, String remotePath) throws Exception {
        if (treeUri == null) {
            ensurePublicDownloadDirectory(remotePath);
            return null;
        }
        Uri parent = rootDocumentUri(treeUri);
        for (String part : pathParts(remotePath)) {
            parent = findOrCreate(context, treeUri, parent, part, DocumentsContract.Document.MIME_TYPE_DIR);
        }
        return parent;
    }

    private static DocumentInfo ensurePublicDownloadFile(Context context, String remotePath, String fallbackName, boolean replaceExisting) throws Exception {
        String[] parts = pathParts(remotePath);
        String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
        String relativeDir = publicRelativeDir(parts);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            DocumentInfo existing = findPublicDownloadFile(context, relativeDir, name);
            if (existing != null && replaceExisting) {
                context.getContentResolver().delete(existing.uri, null, null);
                existing = null;
            }
            if (existing != null) {
                return existing;
            }
            ContentValues values = new ContentValues();
            values.put(MediaStore.Downloads.DISPLAY_NAME, name);
            values.put(MediaStore.Downloads.MIME_TYPE, "application/octet-stream");
            values.put(MediaStore.Downloads.RELATIVE_PATH, relativeDir);
            Uri created = context.getContentResolver().insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values);
            if (created == null) {
                throw new IllegalStateException("Cannot create " + name);
            }
            return new DocumentInfo(created, 0);
        }

        File dir = publicDownloadDir(parts);
        if (!dir.exists() && !dir.mkdirs()) {
            throw new IllegalStateException("Cannot create " + dir.getAbsolutePath());
        }
        File file = new File(dir, name);
        if (file.exists() && replaceExisting && !file.delete()) {
            throw new IllegalStateException("Cannot replace " + file.getAbsolutePath());
        }
        return new DocumentInfo(Uri.fromFile(file), file.exists() ? file.length() : 0);
    }

    private static void ensurePublicDownloadDirectory(String remotePath) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            return;
        }
        File dir = publicDownloadDir(pathParts(remotePath));
        if (!dir.exists()) {
            dir.mkdirs();
        }
    }

    private static DocumentInfo findPublicDownloadFile(Context context, String relativeDir, String name) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            return null;
        }
        String[] columns = {
                MediaStore.Downloads._ID,
                MediaStore.Downloads.SIZE
        };
        String selection = MediaStore.Downloads.DISPLAY_NAME + "=? AND " + MediaStore.Downloads.RELATIVE_PATH + "=?";
        String[] args = {name, ensureTrailingSlash(relativeDir)};
        Cursor cursor = context.getContentResolver().query(MediaStore.Downloads.EXTERNAL_CONTENT_URI, columns, selection, args, null);
        if (cursor == null) {
            return null;
        }
        try {
            if (cursor.moveToFirst()) {
                long id = cursor.getLong(0);
                long size = cursor.isNull(1) ? 0 : cursor.getLong(1);
                Uri uri = Uri.withAppendedPath(MediaStore.Downloads.EXTERNAL_CONTENT_URI, String.valueOf(id));
                return new DocumentInfo(uri, size);
            }
        } finally {
            cursor.close();
        }
        return null;
    }

    private static String publicRelativeDir(String[] parts) {
        StringBuilder builder = new StringBuilder();
        builder.append(Environment.DIRECTORY_DOWNLOADS).append('/').append(DEFAULT_SAVE_FOLDER);
        for (int i = 0; i < parts.length - 1; i++) {
            builder.append('/').append(parts[i]);
        }
        return ensureTrailingSlash(builder.toString());
    }

    private static File publicDownloadDir(String[] parts) {
        File dir = new File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), DEFAULT_SAVE_FOLDER);
        for (int i = 0; i < parts.length - 1; i++) {
            dir = new File(dir, parts[i]);
        }
        return dir;
    }

    private static String ensureTrailingSlash(String value) {
        return value.endsWith("/") ? value : value + "/";
    }

    private static Uri findOrCreate(Context context, Uri treeUri, Uri parent, String name, String mimeType) throws Exception {
        DocumentInfo existing = findChild(context, treeUri, parent, name);
        if (existing != null) {
            return existing.uri;
        }
        Uri created = DocumentsContract.createDocument(context.getContentResolver(), parent, mimeType, name);
        if (created == null) {
            throw new IllegalStateException("Cannot create " + name);
        }
        return created;
    }

    private static DocumentInfo findChild(Context context, Uri treeUri, Uri parent, String name) {
        String parentId = DocumentsContract.getDocumentId(parent);
        Uri children = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId);
        String[] columns = {
                DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                DocumentsContract.Document.COLUMN_SIZE
        };
        Cursor cursor = context.getContentResolver().query(children, columns, null, null, null);
        if (cursor == null) {
            return null;
        }
        try {
            while (cursor.moveToNext()) {
                String childName = cursor.getString(1);
                if (name.equals(childName)) {
                    long size = cursor.isNull(2) ? 0 : cursor.getLong(2);
                    return new DocumentInfo(DocumentsContract.buildDocumentUriUsingTree(treeUri, cursor.getString(0)), size);
                }
            }
        } finally {
            cursor.close();
        }
        return null;
    }

    static String displayName(Context context, Uri uri) {
        try {
            Uri root = rootDocumentUri(uri);
            Cursor cursor = context.getContentResolver().query(root, new String[]{DocumentsContract.Document.COLUMN_DISPLAY_NAME}, null, null, null);
            if (cursor != null) {
                try {
                    if (cursor.moveToFirst()) {
                        String name = cursor.getString(0);
                        if (name != null && !name.trim().isEmpty()) {
                            return name;
                        }
                    }
                } finally {
                    cursor.close();
                }
            }
        } catch (RuntimeException ignored) {
        }
        return context.getString(R.string.selected_folder);
    }

    private static Uri rootDocumentUri(Uri treeUri) {
        return DocumentsContract.buildDocumentUriUsingTree(treeUri, DocumentsContract.getTreeDocumentId(treeUri));
    }

    private static String[] pathParts(String path) {
        String clean = path == null ? "" : path.trim();
        while (clean.startsWith("/")) {
            clean = clean.substring(1);
        }
        if (clean.isEmpty() || ".".equals(clean)) {
            return new String[0];
        }
        String[] raw = clean.split("/");
        List<String> parts = new ArrayList<>();
        for (String part : raw) {
            part = safeName(part);
            if (!part.isEmpty() && !".".equals(part) && !"..".equals(part)) {
                parts.add(part);
            }
        }
        return parts.toArray(new String[0]);
    }

    private static String safeName(String value) {
        String name = value == null ? "" : value.trim();
        name = name.replace('/', '_').replace('\\', '_').replace(':', '_');
        return name.isEmpty() ? "received-file" : name;
    }

    /**
     * Whether {@code candidate} is {@code base} itself or a collision-renamed
     * sibling sharing the same extension, e.g. {@code stem (1)ext}, {@code stem(1)ext},
     * {@code stem-1ext}. Different OEM file pickers use slightly different separators,
     * so accept an optional separator + parens around the digits.
     */
    private static boolean isNameVariant(String candidate, String base) {
        if (candidate == null) {
            return false;
        }
        if (candidate.equals(base)) {
            return true;
        }
        int dot = base.lastIndexOf('.');
        String baseStem = dot > 0 ? base.substring(0, dot) : base;
        String ext = dot > 0 ? base.substring(dot) : "";
        String candStem;
        if (ext.isEmpty()) {
            if (candidate.indexOf('.') >= 0) {
                return false; // candidate gained an extension the base lacks
            }
            candStem = candidate;
        } else {
            if (!candidate.endsWith(ext)) {
                return false;
            }
            candStem = candidate.substring(0, candidate.length() - ext.length());
        }
        if (!candStem.startsWith(baseStem)) {
            return false;
        }
        return isCollisionSuffix(candStem.substring(baseStem.length()));
    }

    /** Accept "(1)", " (2)", "(3)", "-4", "_5", " 6" — the numeric tails added on collision. */
    private static boolean isCollisionSuffix(String suffix) {
        String s = suffix.trim();
        if (s.startsWith("(") && s.endsWith(")")) {
            s = s.substring(1, s.length() - 1).trim();
        } else {
            while (!s.isEmpty() && (s.charAt(0) == '-' || s.charAt(0) == '_' || s.charAt(0) == ' ')) {
                s = s.substring(1);
            }
        }
        if (s.isEmpty()) {
            return false;
        }
        for (int i = 0; i < s.length(); i++) {
            if (!Character.isDigit(s.charAt(i))) {
                return false;
            }
        }
        return true;
    }

    private static String resolveUrl(String serverUrl, String path) throws Exception {
        String base = serverUrl.endsWith("/") ? serverUrl.substring(0, serverUrl.length() - 1) : serverUrl;
        String[] parts = pathParts(path);
        StringBuilder builder = new StringBuilder(base);
        for (String part : parts) {
            builder.append('/');
            builder.append(URLEncoder.encode(part, "UTF-8").replace("+", "%20"));
        }
        return builder.toString();
    }

    private static List<RemoteFile> dropRootDirectories(List<RemoteFile> files) {
        List<RemoteFile> out = new ArrayList<>();
        for (RemoteFile file : files) {
            if (file.isDir) {
                String path = file.path == null ? "" : file.path.trim();
                if (path.isEmpty() || "/".equals(path) || ".".equals(path)) {
                    continue;
                }
            }
            out.add(file);
        }
        return out;
    }

    static String formatBytes(long value) {
        String[] units = {"B", "KB", "MB", "GB", "TB"};
        double size = Math.max(0, value);
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            size /= 1024;
            unit++;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    private static final class TargetResolver {
        private final Context context;
        private final ContentResolver resolver;
        private final Uri treeUri;
        private final SharedPreferences targetPrefs;
        private final String saveKeyPrefix;
        private final Map<String, Uri> treeDirectoryCache = new LinkedHashMap<>();
        private final Map<String, Map<String, DocumentInfo>> treeChildrenCache = new LinkedHashMap<>();
        private final Map<String, Map<String, DocumentInfo>> publicFilesCache = new LinkedHashMap<>();

        TargetResolver(Context context, Uri treeUri) {
            this.context = context;
            this.resolver = context.getContentResolver();
            this.treeUri = treeUri;
            this.targetPrefs = context.getSharedPreferences("gonc_download_targets", Context.MODE_PRIVATE);
            this.saveKeyPrefix = treeUri == null ? "default" : treeUri.toString();
            if (treeUri != null) {
                treeDirectoryCache.put("", rootDocumentUri(treeUri));
            }
        }

        // --- resumable target memory --------------------------------------
        // Remember the exact destination Uri we created for each (save location +
        // remote path), so a resume continues that same file regardless of how the
        // OS renamed it on a name collision ("name (1)", "name(1)", ...) or whether
        // scoped storage hides a same-named file made by another app.

        private String targetKey(String remotePath) {
            return saveKeyPrefix + "\n" + normalizePath(remotePath);
        }

        private DocumentInfo rememberedTarget(String remotePath) {
            String uriStr = targetPrefs.getString(targetKey(remotePath), null);
            if (uriStr == null) {
                return null;
            }
            Uri uri = Uri.parse(uriStr);
            long size = uriSize(uri);
            if (size < 0) {
                // The file is gone (user deleted it / record stale) — forget it.
                targetPrefs.edit().remove(targetKey(remotePath)).apply();
                return null;
            }
            return new DocumentInfo(uri, size);
        }

        private void rememberTarget(String remotePath, Uri uri) {
            if (uri != null) {
                targetPrefs.edit().putString(targetKey(remotePath), uri.toString()).apply();
            }
        }

        private long uriSize(Uri uri) {
            try {
                if ("file".equals(uri.getScheme())) {
                    File file = new File(uri.getPath());
                    return file.exists() ? file.length() : -1;
                }
                Cursor cursor = resolver.query(uri, new String[]{OpenableColumns.SIZE}, null, null, null);
                if (cursor == null) {
                    return -1;
                }
                try {
                    if (cursor.moveToFirst()) {
                        return cursor.isNull(0) ? 0 : cursor.getLong(0);
                    }
                    return -1;
                } finally {
                    cursor.close();
                }
            } catch (Exception error) {
                return -1;
            }
        }

        DocumentInfo ensureFile(String remotePath, String fallbackName, boolean replaceExisting) throws Exception {
            if (treeUri == null) {
                return ensurePublicFile(remotePath, fallbackName, replaceExisting);
            }
            String[] parts = pathParts(remotePath);
            String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
            if (!replaceExisting) {
                DocumentInfo remembered = rememberedTarget(remotePath);
                if (remembered != null) {
                    return remembered;
                }
            }
            Uri parent = ensureTreeDirectory(parentPath(parts));
            DocumentInfo existing = treeChild(parent, name);
            if (existing != null && replaceExisting) {
                DocumentsContract.deleteDocument(resolver, existing.uri);
                treeChildren(parent).remove(name);
                existing = null;
            }
            if (existing != null) {
                rememberTarget(remotePath, existing.uri);
                return existing;
            }
            Uri created = DocumentsContract.createDocument(resolver, parent, "application/octet-stream", name);
            if (created == null) {
                throw new IllegalStateException("Cannot create " + name);
            }
            DocumentInfo info = new DocumentInfo(created, 0);
            treeChildren(parent).put(name, info);
            rememberTarget(remotePath, created);
            return info;
        }

        Uri ensureDirectory(String remotePath) throws Exception {
            if (treeUri == null) {
                ensurePublicDownloadDirectory(remotePath);
                return null;
            }
            return ensureTreeDirectory(pathParts(remotePath));
        }

        private Uri ensureTreeDirectory(String[] parts) throws Exception {
            Uri parent = treeDirectoryCache.get("");
            String currentPath = "";
            for (String part : parts) {
                currentPath = currentPath.isEmpty() ? part : currentPath + "/" + part;
                Uri cached = treeDirectoryCache.get(currentPath);
                if (cached != null) {
                    parent = cached;
                    continue;
                }
                DocumentInfo existing = treeChild(parent, part);
                if (existing != null) {
                    parent = existing.uri;
                } else {
                    Uri created = DocumentsContract.createDocument(resolver, parent, DocumentsContract.Document.MIME_TYPE_DIR, part);
                    if (created == null) {
                        throw new IllegalStateException("Cannot create " + part);
                    }
                    existing = new DocumentInfo(created, 0);
                    treeChildren(parent).put(part, existing);
                    parent = created;
                }
                treeDirectoryCache.put(currentPath, parent);
            }
            return parent;
        }

        private DocumentInfo treeChild(Uri parent, String name) {
            return treeChildren(parent).get(name);
        }

        private Map<String, DocumentInfo> treeChildren(Uri parent) {
            String parentId = DocumentsContract.getDocumentId(parent);
            Map<String, DocumentInfo> cached = treeChildrenCache.get(parentId);
            if (cached != null) {
                return cached;
            }
            Map<String, DocumentInfo> childrenMap = new LinkedHashMap<>();
            Uri children = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentId);
            String[] columns = {
                    DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                    DocumentsContract.Document.COLUMN_DISPLAY_NAME,
                    DocumentsContract.Document.COLUMN_SIZE
            };
            Cursor cursor = resolver.query(children, columns, null, null, null);
            if (cursor != null) {
                try {
                    while (cursor.moveToNext()) {
                        String childName = cursor.getString(1);
                        if (childName == null) {
                            continue;
                        }
                        long size = cursor.isNull(2) ? 0 : cursor.getLong(2);
                        Uri childUri = DocumentsContract.buildDocumentUriUsingTree(treeUri, cursor.getString(0));
                        childrenMap.put(childName, new DocumentInfo(childUri, size));
                    }
                } finally {
                    cursor.close();
                }
            }
            treeChildrenCache.put(parentId, childrenMap);
            return childrenMap;
        }

        private DocumentInfo ensurePublicFile(String remotePath, String fallbackName, boolean replaceExisting) throws Exception {
            String[] parts = pathParts(remotePath);
            String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                String relativeDir = publicRelativeDir(parts);
                Map<String, DocumentInfo> files = publicFiles(relativeDir);
                if (replaceExisting) {
                    DocumentInfo existing = files.get(name);
                    if (existing != null) {
                        resolver.delete(existing.uri, null, null);
                        files.remove(name);
                    }
                } else {
                    // Resume: continue the exact file we created last time, looked up by
                    // the Uri we persisted for this remote path. Independent of MediaStore's
                    // collision rename ("name (1)", "name(1)", ... — OEM dependent) and of
                    // scoped-storage hiding a same-named file made by another app.
                    DocumentInfo remembered = rememberedTarget(remotePath);
                    if (remembered != null) {
                        return remembered;
                    }
                    // Fallback when no record exists yet (e.g. app data cleared): reuse our
                    // own plain name or a collision-renamed sibling, most-complete first.
                    DocumentInfo variant = bestResumeVariant(files, name);
                    if (variant != null) {
                        rememberTarget(remotePath, variant.uri);
                        return variant;
                    }
                }
                ContentValues values = new ContentValues();
                values.put(MediaStore.Downloads.DISPLAY_NAME, name);
                values.put(MediaStore.Downloads.MIME_TYPE, "application/octet-stream");
                values.put(MediaStore.Downloads.RELATIVE_PATH, relativeDir);
                Uri created = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values);
                if (created == null) {
                    throw new IllegalStateException("Cannot create " + name);
                }
                DocumentInfo info = new DocumentInfo(created, 0);
                files.put(name, info);
                rememberTarget(remotePath, created);
                return info;
            }

            File dir = publicDownloadDir(parts);
            if (!dir.exists() && !dir.mkdirs()) {
                throw new IllegalStateException("Cannot create " + dir.getAbsolutePath());
            }
            File file = new File(dir, name);
            if (file.exists() && replaceExisting && !file.delete()) {
                throw new IllegalStateException("Cannot replace " + file.getAbsolutePath());
            }
            return new DocumentInfo(Uri.fromFile(file), file.exists() ? file.length() : 0);
        }

        private Map<String, DocumentInfo> publicFiles(String relativeDir) {
            String normalizedDir = ensureTrailingSlash(relativeDir);
            Map<String, DocumentInfo> cached = publicFilesCache.get(normalizedDir);
            if (cached != null) {
                return cached;
            }
            Map<String, DocumentInfo> files = new LinkedHashMap<>();
            String[] columns = {
                    MediaStore.Downloads._ID,
                    MediaStore.Downloads.DISPLAY_NAME,
                    MediaStore.Downloads.SIZE
            };
            // Some devices store RELATIVE_PATH with a trailing slash, some without; match both.
            String withoutSlash = normalizedDir.endsWith("/") ? normalizedDir.substring(0, normalizedDir.length() - 1) : normalizedDir;
            String selection = MediaStore.Downloads.RELATIVE_PATH + "=? OR " + MediaStore.Downloads.RELATIVE_PATH + "=?";
            String[] args = {normalizedDir, withoutSlash};
            Cursor cursor = resolver.query(MediaStore.Downloads.EXTERNAL_CONTENT_URI, columns, selection, args, null);
            if (cursor != null) {
                try {
                    while (cursor.moveToNext()) {
                        String name = cursor.getString(1);
                        if (name == null) {
                            continue;
                        }
                        long id = cursor.getLong(0);
                        long size = cursor.isNull(2) ? 0 : cursor.getLong(2);
                        Uri uri = Uri.withAppendedPath(MediaStore.Downloads.EXTERNAL_CONTENT_URI, String.valueOf(id));
                        files.put(name, new DocumentInfo(uri, size));
                    }
                } finally {
                    cursor.close();
                }
            }
            publicFilesCache.put(normalizedDir, files);
            return files;
        }

        /**
         * Pick the existing file to resume among our own downloads in this folder:
         * the plain name or a "name (n)" collision-rename, choosing the largest so a
         * completed copy short-circuits and the most-progressed partial is continued.
         */
        private DocumentInfo bestResumeVariant(Map<String, DocumentInfo> files, String name) {
            DocumentInfo best = null;
            for (Map.Entry<String, DocumentInfo> entry : files.entrySet()) {
                if (isNameVariant(entry.getKey(), name) && (best == null || entry.getValue().size > best.size)) {
                    best = entry.getValue();
                }
            }
            return best;
        }

        private static String[] parentPath(String[] parts) {
            if (parts.length <= 1) {
                return new String[0];
            }
            String[] parent = new String[parts.length - 1];
            System.arraycopy(parts, 0, parent, 0, parts.length - 1);
            return parent;
        }
    }

    static final class Session {
        private volatile boolean stopped;
        private volatile HttpURLConnection activeConnection;

        void stop() {
            stopped = true;
            HttpURLConnection conn = activeConnection;
            if (conn != null) {
                conn.disconnect();
            }
        }

        boolean isStopped() {
            return stopped;
        }

        void attach(HttpURLConnection conn) {
            activeConnection = conn;
            if (stopped && conn != null) {
                conn.disconnect();
            }
        }

        void detach(HttpURLConnection conn) {
            if (activeConnection == conn) {
                activeConnection = null;
            }
            if (conn != null) {
                conn.disconnect();
            }
        }
    }

    private static final class ProgressClock {
        private final long startMs = System.currentTimeMillis();
        private long lastMs = startMs;
        private long lastBytes;
        private double bytesPerSecond;

        boolean shouldEmit(long doneBytes) {
            long now = System.currentTimeMillis();
            long elapsed = now - lastMs;
            if (elapsed < 1000 && doneBytes != lastBytes) {
                return false;
            }
            if (elapsed > 0) {
                bytesPerSecond = Math.max(0, (doneBytes - lastBytes) * 1000.0 / elapsed);
            }
            lastMs = now;
            lastBytes = doneBytes;
            return true;
        }

        double speedFor(long doneBytes) {
            if (bytesPerSecond > 0) {
                return bytesPerSecond;
            }
            long elapsed = System.currentTimeMillis() - startMs;
            if (elapsed <= 0 || doneBytes <= 0) {
                return 0;
            }
            return Math.max(0, doneBytes * 1000.0 / elapsed);
        }
    }

    static final class RemoteFile {
        String name;
        boolean isDir;
        long size;
        String path;
        long modifiedMs; // epoch millis of last modification; 0 when unknown
    }

    private static final class ListResult {
        final List<RemoteFile> files;
        final boolean missing;

        ListResult(List<RemoteFile> files, boolean missing) {
            this.files = files;
            this.missing = missing;
        }
    }

    private static final class DocumentInfo {
        final Uri uri;
        final long size;

        DocumentInfo(Uri uri, long size) {
            this.uri = uri;
            this.size = size;
        }
    }

    private static final class DownloadResult {
        final long doneBytes;
        final long networkBytes;
        final boolean skipped;
        final boolean resumed;

        DownloadResult(long doneBytes, long networkBytes, boolean skipped, boolean resumed) {
            this.doneBytes = doneBytes;
            this.networkBytes = networkBytes;
            this.skipped = skipped;
            this.resumed = resumed;
        }
    }
}
