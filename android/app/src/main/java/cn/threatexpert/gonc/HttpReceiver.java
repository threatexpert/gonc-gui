package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.ContentValues;
import android.content.Context;
import android.content.SharedPreferences;
import android.content.res.AssetFileDescriptor;
import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import android.provider.MediaStore;
import android.provider.OpenableColumns;
import android.util.Log;
import android.webkit.MimeTypeMap;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.InterruptedIOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.channels.FileChannel;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.function.Function;
import java.util.zip.GZIPInputStream;

import com.github.luben.zstd.ZstdInputStream;
import mobilegonc.Mobilegonc;

final class HttpReceiver {
    private static final String DEFAULT_SAVE_FOLDER = "Gonc";
    private static final String TAG = "GoncHttpReceiver";
    private static final String MANIFEST_ALGO = "blake3";
    private static final long DEFAULT_MANIFEST_BLOCK_SIZE = 8L * 1024L * 1024L;
    private static final long MIN_MANIFEST_BLOCK_SIZE = 64L * 1024L;
    private static final long MAX_MANIFEST_BLOCK_SIZE = 64L * 1024L * 1024L;
    private static final int MAX_REPAIR_RANGE_COUNT = 128;
    private static final int REPAIR_PLAN_SPARSE = 1;
    private static final int REPAIR_PLAN_TAIL_RESUME = 2;
    private static final int REPAIR_PLAN_TRUNCATE_ONLY = 3;

    interface ListCallback {
        void onList(List<RemoteFile> files, int fileCount, int dirCount, long totalBytes, boolean missing);

        void onError(Throwable error);
    }

    interface Callback {
        default void onDiagnostic(String message) { }
        void onProgress(int doneFiles, int totalFiles, long doneBytes, long totalBytes, long networkBytes, double bytesPerSecond, String current);

        void onComplete(int totalFiles, long doneBytes, long totalBytes, long networkBytes, int skippedFiles, int resumedFiles, List<DownloadFailure> failures);

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
            } finally {
                session.markTerminated();
            }
        }, "gonc-http-list");
        worker.start();
        return session;
    }

    static Session start(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, Callback callback) {
        return start(context, serverUrl, saveTreeUri, requestedFiles, resume, 4, callback);
    }

    // Internal 1/2/4-worker entry point for controlled comparisons; never exceeds four.
    static Session start(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, int concurrency, Callback callback) {
        Session session = new Session();
        Thread worker = new Thread(() -> {
            try {
                run(context.getApplicationContext(), serverUrl, saveTreeUri, requestedFiles, resume, concurrency, session, callback);
            } finally {
                session.markTerminated();
            }
        }, "gonc-http-receive");
        worker.start();
        return session;
    }

    static Map<String, ReceivedTarget> findReceivedTargets(Context context, Uri tree, List<RemoteFile> files) {
        Map<String, ReceivedTarget> found = new LinkedHashMap<>();
        if (context == null || files == null || files.isEmpty()) {
            return found;
        }
        Context applicationContext = context.getApplicationContext();
        TargetResolver resolver = new TargetResolver(applicationContext == null ? context : applicationContext, tree);
        for (RemoteFile file : files) {
            if (file == null || file.isDir) {
                continue;
            }
            DocumentInfo info = resolver.findExisting(file.path, file.name, file.size);
            if (info != null) {
                String name = info.displayName;
                if (name == null || name.isEmpty()) {
                    name = receivedFileName(context.getContentResolver(), info.uri);
                }
                found.put(normalizePath(file.path), new ReceivedTarget(
                        info.uri, name == null ? file.name : name, info.size, info.modifiedMs));
            }
        }
        return found;
    }

    private static void run(Context context, String serverUrl, Uri saveTreeUri, List<RemoteFile> requestedFiles, boolean resume, int concurrency, Session session, Callback callback) {
        int workers = Math.max(1, Math.min(ReceiveWorkerPool.MAX_WORKERS, concurrency));
        try {
            List<RemoteFile> files = requestedFiles == null ? dropRootDirectories(listMany(serverUrl, null, session).files) : new ArrayList<>(requestedFiles);
            List<RemoteFile> jobs = new ArrayList<>();
            Set<String> paths = new HashSet<>();
            TargetResolver targetResolver = new TargetResolver(context, saveTreeUri);
            for (RemoteFile file : files) {
                checkStopped(session);
                if (file.isDir && file.linkError == null) {
                    targetResolver.ensureDirectory(file.path);
                } else if (paths.add(normalizePath(file.path))) {
                    // Repair can update metadata: keep it private to this download worker.
                    RemoteFile copy = new RemoteFile();
                    copy.path = file.path;
                    copy.name = file.name;
                    copy.linkTarget = file.linkTarget;
                    copy.linkError = file.linkError;
                    copy.size = file.size;
                    copy.modifiedMs = file.modifiedMs;
                    jobs.add(copy);
                }
            }
            workers = Math.min(workers, Math.max(1, jobs.size()));
            if (callback != null && !session.isStopped()) callback.onDiagnostic("Receive workers=" + workers);
            BatchProgress batch = new BatchProgress(jobs, session, callback);
            batch.emit("", true);
            ReceiveWorkerPool.run(jobs.size(), workers, session, index -> {
                RemoteFile file = jobs.get(index);
                ProgressClock clock = new ProgressClock();
                Callback fileCallback = new Callback() {
                    @Override public void onProgress(int doneFiles, int totalFiles, long bytes, long total,
                                                     long network, double speed, String current) {
                        batch.update(index, file, bytes, clock.networkBytes);
                    }
                    @Override public void onComplete(int totalFiles, long doneBytes, long totalBytes,
                                                     long networkBytes, int skipped, int resumed, List<DownloadFailure> failures) { }
                    @Override public void onError(Throwable error) { }
                };
                long started = System.nanoTime();
                try {
                    checkStopped(session);
                    if (file.linkError != null) throw new IOException(file.linkError);
                    if (!file.linkTarget.isEmpty() && file.size < 0) throw new IOException("Link target unavailable: " + file.path);
                    DownloadResult result = downloadOne(context, serverUrl, file, targetResolver, resume, session,
                            fileCallback, clock, 0, jobs.size(), 0, 0, Math.max(0, file.size));
                    if (!session.isStopped()) batch.finish(index, file, result.doneBytes, clock.networkBytes,
                            result.skipped, result.resumed, null);
                } catch (Throwable error) {
                    if (!session.isStopped()) batch.finish(index, file, clock.logicalBytes, clock.networkBytes,
                            false, false, new DownloadFailure(file.path, errorMessage(error)));
                } finally {
                    session.timings.record("file", started);
                }
            });
            batch.emit("", true);
            if (callback != null && !session.isStopped()) {
                ReceiveProgress.Snapshot result = batch.progress.snapshot();
                callback.onComplete(result.totalFiles, result.doneBytes, result.totalBytes, result.networkBytes,
                        result.skippedFiles, result.resumedFiles, new ArrayList<>(batch.failures));
            }
        } catch (Throwable error) {
            if (callback != null && !session.isStopped()) {
                callback.onError(error);
            }
        } finally {
            String summary = session.timings.summary(workers);
            Log.i(TAG, summary);
            if (callback != null) callback.onDiagnostic(summary);
        }
    }

    private static void checkStopped(Session session) throws InterruptedIOException {
        if (session.isStopped() || Thread.currentThread().isInterrupted()) throw new InterruptedIOException("Receive stopped");
    }

    private static final class BatchProgress {
        final ReceiveProgress progress;
        final List<DownloadFailure> failures = new ArrayList<>();
        private final Session session;
        private final Callback callback;
        private long lastEmit = System.nanoTime();
        private long lastNetwork;
        private double speed;

        BatchProgress(List<RemoteFile> files, Session session, Callback callback) {
            long[] sizes = new long[files.size()];
            for (int i = 0; i < sizes.length; i++) sizes[i] = files.get(i).size;
            progress = new ReceiveProgress(sizes);
            this.session = session;
            this.callback = callback;
        }

        synchronized void update(int index, RemoteFile file, long bytes, long network) {
            progress.update(index, bytes, network, file.size);
            emit(file.path, false);
        }

        synchronized void finish(int index, RemoteFile file, long bytes, long network,
                                 boolean skipped, boolean resumed, DownloadFailure failure) {
            progress.finish(index, bytes, network, file.size, skipped, resumed, failure != null);
            if (failure != null) failures.add(failure);
            emit(file.path, false);
        }

        synchronized void emit(String current, boolean force) {
            if (callback == null || session.isStopped()) return;
            long now = System.nanoTime();
            long elapsed = now - lastEmit;
            if (!force && elapsed < 200_000_000L) return;
            ReceiveProgress.Snapshot s = progress.snapshot();
            if (elapsed > 0) speed = Math.max(0, (s.networkBytes - lastNetwork) * 1_000_000_000.0 / elapsed);
            lastEmit = now;
            lastNetwork = s.networkBytes;
            callback.onProgress(s.doneFiles, s.totalFiles, s.doneBytes, s.totalBytes, s.networkBytes, speed, current);
        }
    }

    private static String errorMessage(Throwable error) {
        if (error == null) {
            return "unknown error";
        }
        String message = error.getMessage();
        return message == null || message.trim().isEmpty() ? error.toString() : message;
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
        // A document tree cannot store filesystem symlinks. Expand directory
        // targets before selection so excluding a child remains effective.
        List<RemoteFile> pending = new ArrayList<>(merged.values());
        for (int depth = 0; !pending.isEmpty(); depth++) {
            List<RemoteFile> next = new ArrayList<>();
            for (RemoteFile dir : pending) {
                if (!dir.isDir || dir.linkTarget.isEmpty()) continue;
                checkStopped(session);
                try {
                    if (depth >= 64) throw new IOException("Directory link expansion too deep: " + dir.path);
                    ListResult listing = list(serverUrl, dir.path, session);
                    boolean hasRoot = false;
                    for (RemoteFile child : listing.files) {
                        if (dir.path.equals(child.path) && child.isDir && child.linkTarget.isEmpty() && !hasRoot) {
                            hasRoot = true;
                        } else if (!child.path.startsWith(dir.path + "/") || child.path.contains("/../")
                                || child.path.endsWith("/..") || child.path.indexOf('\\') >= 0) {
                            throw new IOException("Invalid directory link listing: " + dir.path);
                        }
                    }
                    if (!hasRoot) throw new IOException("Directory link target unavailable: " + dir.path);
                    for (RemoteFile child : listing.files) {
                        if (merged.putIfAbsent(normalizePath(child.path), child) == null && child.isDir && !child.linkTarget.isEmpty()) next.add(child);
                    }
                } catch (Exception err) {
                    checkStopped(session);
                    dir.linkError = err.toString();
                    dir.size = -1;
                }
            }
            pending = next;
        }
        return new ListResult(new ArrayList<>(merged.values()), requested > 0 && missing == requested);
    }

    private static ListResult list(String serverUrl, String subPath, Session session) throws Exception {
        HttpURLConnection conn = open(resolveUrl(serverUrl, subPath));
        session.attach(conn);
        try {
            checkStopped(session);
            conn.setRequestProperty("Accept", "application/json");
            conn.setRequestProperty("Accept-Encoding", "zstd, gzip");
            int status = conn.getResponseCode();
            if (status == HttpURLConnection.HTTP_NOT_FOUND) return new ListResult(new ArrayList<>(), true);
            if (status != HttpURLConnection.HTTP_OK) {
                throw new IllegalStateException("Remote list failed: HTTP " + status);
            }
            List<RemoteFile> files = new ArrayList<>();
            try (InputStream input = decodedInputStream(conn, false);
                 BufferedReader reader = new BufferedReader(new InputStreamReader(input, StandardCharsets.UTF_8))) {
                String line;
                while ((line = reader.readLine()) != null) {
                    checkStopped(session);
                    line = line.trim();
                    if (line.isEmpty()) continue;
                    JSONObject json = new JSONObject(line);
                    RemoteFile file = new RemoteFile();
                    file.name = json.optString("name");
                    file.isDir = json.optBoolean("is_dir");
                    file.linkTarget = json.optString("link_target");
                    file.size = json.optLong("size");
                    file.path = json.optString("path");
                    file.modifiedMs = parseModTime(json.optString("mod_time"));
                    if (file.path == null || file.path.trim().isEmpty()) file.path = file.name;
                    files.add(file);
                }
            }
            return new ListResult(files, false);
        } finally {
            session.detach(conn);
        }
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
        long started = System.nanoTime();
        DocumentInfo target;
        try {
            target = targetResolver.ensureFile(file.path, file.name, !resume, session);
        } finally {
            session.timings.record("target", started);
        }
        checkStopped(session);
        String downloadUrl = resolveUrl(serverUrl, file.path);
        if (resume && target.size == file.size && file.size >= 0 && target.modifiedMs > 0 && file.modifiedMs > 0 && target.modifiedMs == file.modifiedMs) {
            return new DownloadResult(file.size, 0, true, false);
        }

        if (resume && target.size > 0) {
            started = System.nanoTime();
            DownloadResult repaired;
            try {
                repaired = tryRepair(context, downloadUrl, file, target, session, callback, clock, doneFiles, totalFiles, baseDoneBytes, baseNetworkBytes, totalBytes);
            } finally {
                session.timings.record("repair", started);
            }
            if (repaired != null) {
                return repaired;
            }
            checkStopped(session);
            Log.i(TAG, "BLAKE3 repair unavailable or inefficient for " + file.path + "; re-downloading full file");
        }

        return downloadFull(context, downloadUrl, file, target, session, callback, clock, doneFiles, totalFiles, baseDoneBytes, baseNetworkBytes, totalBytes);
    }

    private static DownloadResult downloadFull(Context context, String downloadUrl, RemoteFile file, DocumentInfo target, Session session, Callback callback, ProgressClock clock, int doneFiles, int totalFiles, long baseDoneBytes, long baseNetworkBytes, long totalBytes) throws Exception {
        HttpURLConnection conn = open(downloadUrl);
        session.attach(conn);
        conn.setRequestProperty("Accept-Encoding", "zstd, gzip");
        try {
            checkStopped(session);
            long started = System.nanoTime();
            int status;
            try {
                status = conn.getResponseCode();
            } finally {
                session.timings.record("headers", started);
            }
            if (status != HttpURLConnection.HTTP_OK) {
                throw new IllegalStateException("Download failed for " + file.path + ": HTTP " + status);
            }

            long copied = 0;
            ContentResolver resolver = context.getContentResolver();
            checkStopped(session);
            started = System.nanoTime();
            try (InputStream input = decodedInputStream(conn, false); OutputStream output = openTargetOutput(resolver, target.uri, session)) {
                if (output == null) {
                    throw new IllegalStateException("Cannot open destination: " + file.path);
                }
                clock.logicalBytes = 0;
                if (callback != null) callback.onProgress(doneFiles, totalFiles, baseDoneBytes, totalBytes,
                        clock.networkBytes, clock.speedFor(clock.networkBytes), file.path);
                byte[] buffer = new byte[128 * 1024];
                int n;
                while ((n = input.read(buffer)) >= 0) {
                    checkStopped(session);
                    clock.networkBytes += n;
                    output.write(buffer, 0, n);
                    copied += n;
                    clock.logicalBytes = copied;
                    if (callback != null && clock.shouldEmit(clock.networkBytes)) {
                        callback.onProgress(doneFiles, totalFiles, baseDoneBytes + copied, totalBytes, clock.networkBytes, clock.speedFor(clock.networkBytes), file.path);
                    }
                }
            } finally {
                session.timings.record("bodyAndSave", started);
            }
            if (file.size >= 0 && copied != file.size) {
                throw new IllegalStateException("Downloaded size mismatch for " + file.path + ": got " + copied + ", expected " + file.size);
            }
            if (file.size < 0) file.size = copied;
            checkStopped(session);
            setTargetModifiedTimed(context, target.uri, file.modifiedMs, file.path, session);
            return new DownloadResult(copied, clock.networkBytes, false, false);
        } finally {
            session.detach(conn);
        }
    }

    private static OutputStream openTargetOutput(ContentResolver resolver, Uri uri, Session session) throws Exception {
        long started = System.nanoTime();
        try {
            checkStopped(session);
            return resolver.openOutputStream(uri, "wt");
        } finally {
            session.timings.record("openOutput", started);
        }
    }

    private static void setTargetModifiedTimed(Context context, Uri uri, long modifiedMs, String label, Session session) {
        long started = System.nanoTime();
        try {
            setTargetModifiedMs(context, uri, modifiedMs, label);
        } finally {
            session.timings.record("modified", started);
        }
    }

    private static DownloadResult tryRepair(Context context, String downloadUrl, RemoteFile file, DocumentInfo target, Session session, Callback callback, ProgressClock clock, int doneFiles, int totalFiles, long baseDoneBytes, long baseNetworkBytes, long totalBytes) throws Exception {
        RepairManifest manifest = null;
        RepairPlan plan = null;

        if (target.size < file.size) {
            manifest = fetchManifest(downloadUrl, session, target.size);
            if (manifest == null) {
                manifest = fetchManifest(downloadUrl, session, 0);
            }
            if (manifest == null) {
                Log.i(TAG, "Repair check unavailable for " + file.path + ": server does not provide BLAKE3 manifest");
                return null;
            }
            file.size = manifest.size;
            file.modifiedMs = manifest.modifiedMs;
            if (target.size < manifest.size) {
                try {
                    plan = planTailResume(context, target, manifest);
                } catch (Exception error) {
                    Log.i(TAG, "Tail resume planning unavailable for " + file.path + ": " + error.getMessage());
                    plan = null;
                }
                if (plan == null) {
                    Log.i(TAG, "Tail resume check for " + file.path + " did not prove a clean prefix; requesting full repair manifest");
                    if (!manifest.isFull()) {
                        manifest = fetchManifest(downloadUrl, session, 0);
                        if (manifest == null) {
                            Log.i(TAG, "Repair check unavailable for " + file.path + ": server does not provide BLAKE3 manifest");
                            return null;
                        }
                        file.size = manifest.size;
                        file.modifiedMs = manifest.modifiedMs;
                    }
                }
            }
        }

        if (manifest == null) {
            manifest = fetchManifest(downloadUrl, session, 0);
        }
        if (manifest == null) {
            Log.i(TAG, "Repair check unavailable for " + file.path + ": server does not provide BLAKE3 manifest");
            return null;
        }
        file.size = manifest.size;
        file.modifiedMs = manifest.modifiedMs;

        if (plan == null) {
            try {
                plan = planRepair(context, target, manifest);
            } catch (Exception error) {
                Log.i(TAG, "Repair planning unavailable for " + file.path + ": " + error.getMessage());
                return null;
            }
        }
        if (plan.kind == REPAIR_PLAN_SPARSE && shouldRedownload(manifest.size, plan.dirtyBytes, plan.dirtyRangeCount)) {
            Log.i(TAG, "Repair check for " + file.path + ": local=" + formatBytes(target.size) + " remote=" + formatBytes(manifest.size)
                    + ", " + plan.dirtyRangeCount + " dirty range(s) totaling " + formatBytes(plan.dirtyBytes)
                    + "; full download selected");
            return null;
        }

        long keptBytes = Math.max(0, manifest.size - plan.transferBytes);
        clock.logicalBytes = keptBytes;
        if (callback != null && keptBytes > 0) {
            callback.onProgress(doneFiles, totalFiles, baseDoneBytes + keptBytes, totalBytes, baseNetworkBytes, clock.speedFor(baseNetworkBytes), file.path);
        }
        Log.i(TAG, "Repair plan for " + file.path + ": kind=" + repairPlanKindName(plan.kind)
                + ", local=" + formatBytes(target.size) + " remote=" + formatBytes(manifest.size)
                + ", keep " + formatBytes(keptBytes) + ", download " + formatBytes(plan.transferBytes)
                + " in " + plan.ranges.size() + " range request(s), dirty " + formatBytes(plan.dirtyBytes)
                + " in " + plan.dirtyRangeCount + " range(s)");

        long downloaded = 0;
        try (ParcelFileDescriptor pfd = context.getContentResolver().openFileDescriptor(target.uri, "rw")) {
            if (pfd == null) {
                return null;
            }
            try (FileOutputStream stream = new FileOutputStream(pfd.getFileDescriptor()); FileChannel channel = stream.getChannel()) {
                for (RepairRange range : plan.ranges) {
                    checkStopped(session);
                    long written = downloadRange(downloadUrl, file, range, channel, session, callback, clock, doneFiles, totalFiles, baseDoneBytes + keptBytes, baseNetworkBytes, totalBytes, downloaded);
                    downloaded += written;
                }
                checkStopped(session);
                channel.truncate(manifest.size);
            }
        } catch (Exception error) {
            checkStopped(session);
            Log.i(TAG, "Range repair unavailable for " + file.path + ": " + error.getMessage());
            return null;
        }

        if (callback != null) {
            callback.onProgress(doneFiles, totalFiles, baseDoneBytes + manifest.size, totalBytes, baseNetworkBytes + downloaded, clock.speedFor(baseNetworkBytes + downloaded), file.path);
        }
        checkStopped(session);
        setTargetModifiedTimed(context, target.uri, manifest.modifiedMs, file.path, session);
        Log.i(TAG, "Repair completed for " + file.path + ": " + plan.ranges.size() + " range request(s), downloaded " + formatBytes(downloaded));
        return new DownloadResult(manifest.size, clock.networkBytes, downloaded == 0, downloaded > 0);
    }

    private static void setTargetModifiedMs(Context context, Uri uri, long modifiedMs, String label) {
        if (uri == null || modifiedMs <= 0) {
            return;
        }
        try {
            if ("file".equals(uri.getScheme())) {
                File file = new File(uri.getPath());
                if (file.exists() && file.setLastModified(modifiedMs)) {
                    return;
                }
            }
        } catch (Exception ignored) {
        }

        ContentResolver resolver = context.getContentResolver();
        if (MediaStore.AUTHORITY.equals(uri.getAuthority())) {
            // Downloads/MediaStore uses seconds; do not first attempt the DocumentsProvider column.
            if (!tryUpdateModifiedMs(resolver, uri, MediaStore.MediaColumns.DATE_MODIFIED, modifiedMs / 1000L)) {
                Log.i(TAG, "Could not set modified time for " + label + " on " + uri);
            }
            return;
        }
        if (tryUpdateModifiedMs(resolver, uri, DocumentsContract.Document.COLUMN_LAST_MODIFIED, modifiedMs)) {
            return;
        }
        if (tryUpdateModifiedMs(resolver, uri, MediaStore.MediaColumns.DATE_MODIFIED, modifiedMs / 1000L)) {
            return;
        }
        Log.i(TAG, "Could not set modified time for " + label + " on " + uri);
    }

    private static boolean tryUpdateModifiedMs(ContentResolver resolver, Uri uri, String column, long value) {
        try {
            ContentValues values = new ContentValues();
            values.put(column, value);
            return resolver.update(uri, values, null, null) > 0;
        } catch (Exception ignored) {
            return false;
        }
    }

    private static RepairManifest fetchManifest(String downloadUrl, Session session, long limitSize) throws Exception {
        String manifestUrl = downloadUrl + (downloadUrl.contains("?") ? "&" : "?")
                + "manifest=blake3&block_size=" + DEFAULT_MANIFEST_BLOCK_SIZE;
        if (limitSize > 0) {
            manifestUrl += "&limit_size=" + limitSize;
        }
        HttpURLConnection conn = open(manifestUrl);
        session.attach(conn);
        conn.setRequestProperty("Accept", "application/json");
        conn.setRequestProperty("Accept-Encoding", "zstd, gzip");
        try {
            checkStopped(session);
            if (conn.getResponseCode() != HttpURLConnection.HTTP_OK) {
                return null;
            }
            RepairManifest manifest = new RepairManifest();
            try (InputStream input = decodedInputStream(conn, true);
                 BufferedReader reader = new BufferedReader(new InputStreamReader(input, StandardCharsets.UTF_8))) {
                String line;
                while ((line = reader.readLine()) != null) {
                    if (session.isStopped()) {
                        return null;
                    }
                    line = line.trim();
                    if (line.isEmpty()) {
                        continue;
                    }
                    JSONObject json = new JSONObject(line);
                    String type = json.optString("type");
                    if ("file".equals(type)) {
                        if (!MANIFEST_ALGO.equals(json.optString("algo"))) {
                            return null;
                        }
                        manifest.path = json.optString("path");
                        manifest.size = json.optLong("size");
                        manifest.modifiedMs = parseModTime(json.optString("mod_time"));
                        manifest.blockSize = normalizeManifestBlockSize(json.optLong("block_size", DEFAULT_MANIFEST_BLOCK_SIZE));
                        manifest.manifestSize = json.optLong("manifest_size", 0);
                        manifest.limitSize = json.optLong("limit_size", 0);
                    } else if ("block".equals(type)) {
                        manifest.blocks.add(new ManifestBlock(
                                json.optInt("index"),
                                json.optLong("offset"),
                                json.optLong("size"),
                                json.optString("hash")));
                    } else {
                        return null;
                    }
                }
            }
            if (manifest.blockSize <= 0 && manifest.size > 0) {
                return null;
            }
            return manifest;
        } finally {
            session.detach(conn);
        }
    }

    private static RepairPlan planTailResume(Context context, DocumentInfo target, RepairManifest manifest) throws Exception {
        if (target.size >= manifest.size) {
            return null;
        }
        long blockSize = normalizeManifestBlockSize(manifest.blockSize);
        long prefixSize = (target.size / blockSize) * blockSize;
        List<ManifestBlock> localBlocks = localBlockHashes(context, target.uri, prefixSize, blockSize);
        for (ManifestBlock local : localBlocks) {
            ManifestBlock remote = findManifestBlock(manifest.blocks, local.index);
            if (remote == null || remote.offset != local.offset || remote.size != local.size || !remote.hash.equals(local.hash)) {
                return null;
            }
        }
        long transferBytes = manifest.size - prefixSize;
        List<RepairRange> ranges = new ArrayList<>();
        if (transferBytes > 0) {
            ranges.add(new RepairRange(prefixSize, transferBytes));
        }
        return new RepairPlan(ranges, transferBytes, 0, 0, REPAIR_PLAN_TAIL_RESUME);
    }

    private static ManifestBlock findManifestBlock(List<ManifestBlock> blocks, int index) {
        for (ManifestBlock block : blocks) {
            if (block.index == index) {
                return block;
            }
        }
        return null;
    }

    private static RepairPlan planRepair(Context context, DocumentInfo target, RepairManifest manifest) throws Exception {
        List<ManifestBlock> localBlocks = localBlockHashes(context, target.uri, Math.min(Math.max(0, target.size), manifest.size), manifest.blockSize);
        List<RepairRange> ranges = new ArrayList<>();
        List<RepairRange> dirtyRanges = new ArrayList<>();
        for (ManifestBlock remote : manifest.blocks) {
            boolean needsDownload = remote.offset + remote.size > target.size;
            boolean dirty = false;
            if (!needsDownload) {
                if (remote.index >= localBlocks.size()) {
                    needsDownload = true;
                    dirty = true;
                } else {
                    ManifestBlock local = localBlocks.get(remote.index);
                    needsDownload = local.size != remote.size || !remote.hash.equals(local.hash);
                    dirty = needsDownload;
                }
            }
            if (needsDownload) {
                RepairRange range = new RepairRange(remote.offset, remote.size);
                ranges.add(range);
                if (dirty) {
                    dirtyRanges.add(range);
                }
            }
        }
        ranges = mergeRanges(ranges);
        dirtyRanges = mergeRanges(dirtyRanges);
        long transferBytes = 0;
        for (RepairRange range : ranges) {
            transferBytes += range.size;
        }
        long dirtyBytes = 0;
        for (RepairRange range : dirtyRanges) {
            dirtyBytes += range.size;
        }
        int kind = transferBytes == 0 && target.size > manifest.size ? REPAIR_PLAN_TRUNCATE_ONLY : REPAIR_PLAN_SPARSE;
        return new RepairPlan(ranges, transferBytes, dirtyBytes, dirtyRanges.size(), kind);
    }

    private static String repairPlanKindName(int kind) {
        if (kind == REPAIR_PLAN_TAIL_RESUME) {
            return "TailResume";
        }
        if (kind == REPAIR_PLAN_TRUNCATE_ONLY) {
            return "TruncateOnly";
        }
        return "SparseRepair";
    }

    private static List<ManifestBlock> localBlockHashes(Context context, Uri uri, long fileSize, long blockSize) throws Exception {
        List<ManifestBlock> blocks = new ArrayList<>();
        blockSize = normalizeManifestBlockSize(blockSize);
        int bufferSize = (int) blockSize;
        try (InputStream input = openLocalInput(context, uri)) {
            if (input == null) {
                throw new IllegalStateException("Cannot open local file for repair");
            }
            byte[] buffer = new byte[bufferSize];
            long offset = 0;
            int index = 0;
            while (offset < fileSize) {
                if (Thread.currentThread().isInterrupted()) throw new InterruptedIOException("Receive stopped while hashing");
                int want = (int) Math.min(blockSize, fileSize - offset);
                int got = readFully(input, buffer, want);
                if (got != want) {
                    throw new IllegalStateException("Local file ended while hashing");
                }
                byte[] data = got == buffer.length ? buffer : Arrays.copyOf(buffer, got);
                blocks.add(new ManifestBlock(index, offset, got, Mobilegonc.blake3Hex(data)));
                offset += got;
                index++;
            }
        }
        return blocks;
    }

    private static InputStream openLocalInput(Context context, Uri uri) throws Exception {
        if ("file".equals(uri.getScheme())) {
            return new FileInputStream(new File(uri.getPath()));
        }
        return context.getContentResolver().openInputStream(uri);
    }

    private static int readFully(InputStream input, byte[] buffer, int want) throws Exception {
        int total = 0;
        while (total < want) {
            if (Thread.currentThread().isInterrupted()) throw new InterruptedIOException("Receive stopped while reading");
            int n = input.read(buffer, total, want - total);
            if (n < 0) {
                break;
            }
            total += n;
        }
        return total;
    }

    private static long downloadRange(String downloadUrl, RemoteFile file, RepairRange range, FileChannel channel, Session session, Callback callback, ProgressClock clock, int doneFiles, int totalFiles, long baseDoneBytes, long baseNetworkBytes, long totalBytes, long alreadyDownloaded) throws Exception {
        HttpURLConnection conn = open(downloadUrl);
        session.attach(conn);
        conn.setRequestProperty("Range", "bytes=" + range.offset + "-" + range.endInclusive());
        conn.setRequestProperty("Accept-Encoding", "zstd, gzip");
        try {
            checkStopped(session);
            if (conn.getResponseCode() != HttpURLConnection.HTTP_PARTIAL) {
                throw new IllegalStateException("Range request unsupported: HTTP " + conn.getResponseCode());
            }
            channel.position(range.offset);
            long written = 0;
            byte[] buffer = new byte[128 * 1024];
            try (InputStream input = decodedInputStream(conn, true)) {
                int n;
                while ((n = input.read(buffer)) >= 0) {
                    checkStopped(session);
                    clock.networkBytes += n;
                    ByteBuffer chunk = ByteBuffer.wrap(buffer, 0, n);
                    while (chunk.hasRemaining()) {
                        checkStopped(session);
                        channel.write(chunk);
                    }
                    written += n;
                    clock.logicalBytes = baseDoneBytes + alreadyDownloaded + written;
                    if (callback != null && clock.shouldEmit(clock.networkBytes)) {
                        callback.onProgress(doneFiles, totalFiles, baseDoneBytes + alreadyDownloaded + written, totalBytes,
                                clock.networkBytes,
                                clock.speedFor(clock.networkBytes), file.path);
                    }
                }
            }
            if (written != range.size) {
                throw new IllegalStateException("Range size mismatch for " + file.path + ": got " + written + ", expected " + range.size);
            }
            return written;
        } finally {
            session.detach(conn);
        }
    }

    private static InputStream decodedInputStream(HttpURLConnection conn, boolean strict) throws Exception {
        String encoding = conn.getHeaderField("Content-Encoding");
        if (encoding == null) {
            return conn.getInputStream();
        }
        String normalized = encoding.trim().toLowerCase(Locale.ROOT);
        if (normalized.isEmpty() || "identity".equals(normalized)) {
            return conn.getInputStream();
        }
        if ("zstd".equals(normalized)) {
            return new ZstdInputStream(conn.getInputStream());
        }
        if ("gzip".equals(normalized)) {
            return new GZIPInputStream(conn.getInputStream());
        }
        if (strict) {
            throw new IllegalStateException("Unsupported Content-Encoding: " + encoding);
        }
        return conn.getInputStream();
    }

    private static long normalizeManifestBlockSize(long blockSize) {
        if (blockSize <= 0) {
            return DEFAULT_MANIFEST_BLOCK_SIZE;
        }
        if (blockSize < MIN_MANIFEST_BLOCK_SIZE) {
            return MIN_MANIFEST_BLOCK_SIZE;
        }
        if (blockSize > MAX_MANIFEST_BLOCK_SIZE) {
            return MAX_MANIFEST_BLOCK_SIZE;
        }
        return blockSize;
    }

    private static List<RepairRange> mergeRanges(List<RepairRange> ranges) {
        if (ranges.size() < 2) {
            return ranges;
        }
        List<RepairRange> merged = new ArrayList<>();
        for (RepairRange current : ranges) {
            if (merged.isEmpty()) {
                merged.add(current);
                continue;
            }
            RepairRange last = merged.get(merged.size() - 1);
            if (last.offset + last.size == current.offset) {
                last.size += current.size;
            } else {
                merged.add(current);
            }
        }
        return merged;
    }

    private static boolean shouldRedownload(long remoteSize, long transferBytes, int rangeCount) {
        if (remoteSize == 0) {
            return false;
        }
        return rangeCount > MAX_REPAIR_RANGE_COUNT || transferBytes * 2 > remoteSize;
    }

    private static HttpURLConnection open(String url) throws Exception {
        return open(new URL(url));
    }

    static HttpURLConnection open(URL url) throws Exception {
        HttpURLConnection conn = (HttpURLConnection) url.openConnection(Proxy.NO_PROXY);
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

    private static String receivedFileName(ContentResolver resolver, Uri uri) {
        if ("file".equals(uri.getScheme())) {
            return uri.getLastPathSegment();
        }
        try (Cursor cursor = resolver.query(uri, new String[]{OpenableColumns.DISPLAY_NAME}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst()) {
                String name = cursor.getString(0);
                return name == null || name.isEmpty() ? null : name;
            }
        } catch (RuntimeException ignored) {
        }
        return null;
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

    private static int extensionStart(String name) {
        int dot = name.lastIndexOf('.');
        return dot > 0 && dot < name.length() - 1 ? dot : name.length();
    }

    static String numberedFileName(String name, int number) {
        if (number == 0) return name;
        int dot = extensionStart(name);
        return name.substring(0, dot) + " (" + number + ")" + name.substring(dot);
    }

    static String mimeTypeForSave(String name, Function<String, String> lookup) {
        int dot = extensionStart(name);
        if (dot == name.length()) return "application/octet-stream";
        String extension = name.substring(dot + 1).toLowerCase(Locale.ROOT);
        if ("apk".equals(extension)) return ReceivedFileActions.fallbackMimeType(name);
        String type = lookup.apply(extension);
        return type == null || type.isEmpty() ? "application/octet-stream" : type;
    }

    static boolean acceptsCreatedName(String original, String requested, String actual) {
        if (actual == null) return false;
        if (actual.equalsIgnoreCase(requested)) return true;
        int originalDot = extensionStart(original);
        int actualDot = extensionStart(actual);
        if (originalDot < original.length()
                && !original.substring(originalDot).equalsIgnoreCase(actual.substring(actualDot))) {
            return false;
        }
        // Storage can sanitize the stem. Validate the suffix when it kept the original stem.
        if (actualDot < originalDot || !actual.regionMatches(true, 0, original, 0, originalDot)) return true;
        if (!requested.equals(original)) return false;
        String number = actual.substring(originalDot, actualDot);
        return number.matches(" \\([1-9][0-9]*\\)");
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
        private static final int MAX_PROVIDER_NAME_RETRIES = 32;
        private static final String[] FILE_COLUMNS = {OpenableColumns.SIZE, OpenableColumns.DISPLAY_NAME};
        private static final String[] MEDIA_FILE_COLUMNS = {
                OpenableColumns.SIZE, OpenableColumns.DISPLAY_NAME, MediaStore.MediaColumns.DATE_MODIFIED
        };

        private interface CreateTarget {
            Uri create(String name) throws Exception;
        }

        private final Context context;
        private final ContentResolver resolver;
        private final Uri treeUri;
        private final SharedPreferences targetPrefs;
        private final String saveKeyPrefix;
        private final Map<String, Uri> treeDirectoryCache = new LinkedHashMap<>();
        private final Map<String, Map<String, DocumentInfo>> treeChildrenCache = new LinkedHashMap<>();
        private final Map<String, Map<String, DocumentInfo>> publicFilesCache = new LinkedHashMap<>();
        private final ReceiveTargetClaims claims = new ReceiveTargetClaims();

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
        // OS renamed it on a name collision (e.g. "name (1).apk") or whether
        // scoped storage hides a same-named file made by another app.

        private String targetKey(String remotePath) {
            return saveKeyPrefix + "\n" + normalizePath(remotePath);
        }

        private DocumentInfo rememberedTarget(String remotePath) {
            return rememberedTarget(remotePath, true);
        }

        private DocumentInfo rememberedTarget(String remotePath, boolean forgetStale) {
            String uriStr = targetPrefs.getString(targetKey(remotePath), null);
            if (uriStr == null) {
                return null;
            }
            Uri uri = Uri.parse(uriStr);
            DocumentInfo info = uriInfo(uri);
            if (info == null || info.size < 0) {
                // The file is gone (user deleted it / record stale) — forget it.
                if (forgetStale) {
                    targetPrefs.edit().remove(targetKey(remotePath)).apply();
                }
                return null;
            }
            return info;
        }

        private void rememberTarget(String remotePath, Uri uri) {
            if (uri != null) {
                targetPrefs.edit().putString(targetKey(remotePath), uri.toString()).apply();
            }
        }

        private DocumentInfo uriInfo(Uri uri) {
            try {
                if ("file".equals(uri.getScheme())) {
                    File file = new File(uri.getPath());
                    return file.exists()
                            ? new DocumentInfo(uri, file.length(), file.lastModified(), file.isDirectory(), true, file.getName())
                            : null;
                }
                boolean mediaStore = MediaStore.AUTHORITY.equals(uri.getAuthority());
                try {
                    return queryFileInfo(uri, mediaStore ? MEDIA_FILE_COLUMNS : FILE_COLUMNS, mediaStore);
                } catch (RuntimeException ignored) {
                    // Some providers accept only a minimal projection.
                    return queryFileInfo(uri, new String[]{OpenableColumns.SIZE}, mediaStore);
                }
            } catch (Exception error) {
                return null;
            }
        }

        private DocumentInfo queryFileInfo(Uri uri, String[] columns, boolean mediaStore) {
            long size;
            boolean sizeKnown;
            String name;
            int modifiedColumn;
            long modifiedMs;
            try (Cursor cursor = resolver.query(uri, columns, null, null, null)) {
                if (cursor == null || !cursor.moveToFirst()) return null;
                int sizeColumn = cursor.getColumnIndexOrThrow(OpenableColumns.SIZE);
                sizeKnown = !cursor.isNull(sizeColumn);
                size = sizeKnown ? cursor.getLong(sizeColumn) : 0;
                int nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME);
                name = nameColumn < 0 ? null : cursor.getString(nameColumn);
                modifiedColumn = mediaStore ? cursor.getColumnIndex(MediaStore.MediaColumns.DATE_MODIFIED) : -1;
                modifiedMs = modifiedColumn < 0 || cursor.isNull(modifiedColumn) ? 0 : cursor.getLong(modifiedColumn) * 1000L;
            }
            if (modifiedColumn < 0) modifiedMs = queryModifiedMs(uri);
            // The MediaStore URIs tracked here are Downloads items and already represent files.
            boolean directory = !mediaStore && DocumentsContract.Document.MIME_TYPE_DIR.equals(resolver.getType(uri));
            return new DocumentInfo(uri, size, modifiedMs, directory, sizeKnown, name);
        }

        DocumentInfo findExisting(String remotePath, String fallbackName, long expectedSize) {
            DocumentInfo remembered = rememberedTarget(remotePath, false);
            if (isAvailableTarget(remembered, expectedSize)) {
                return remembered;
            }

            String[] parts = pathParts(remotePath);
            String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
            if (treeUri != null) {
                Uri parent = treeDirectoryCache.get("");
                for (String part : parentPath(parts)) {
                    DocumentInfo directory = treeChild(parent, part);
                    if (directory == null || !directory.directory) {
                        return null;
                    }
                    parent = directory.uri;
                }
                return availableVariant(treeChildren(parent), name, expectedSize);
            }

            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                return availableVariant(publicFiles(publicRelativeDir(parts)), name, expectedSize);
            }

            File dir = publicDownloadDir(parts);
            File[] children = dir.listFiles();
            if (children == null) {
                return null;
            }
            Map<String, DocumentInfo> existing = new LinkedHashMap<>();
            for (File child : children) {
                existing.put(child.getName(), new DocumentInfo(
                        Uri.fromFile(child), child.length(), child.lastModified(), child.isDirectory(), true, child.getName()));
            }
            return availableVariant(existing, name, expectedSize);
        }

        private boolean isAvailableTarget(DocumentInfo info, long expectedSize) {
            return info != null && ReceivedFileMatcher.isAvailable(
                    true, canRead(info.uri), info.directory,
                    ReceivedFileMatcher.reportedSize(info.sizeKnown, info.size), expectedSize);
        }

        private DocumentInfo availableVariant(Map<String, DocumentInfo> files, String name, long expectedSize) {
            List<DocumentInfo> candidates = new ArrayList<>();
            for (Map.Entry<String, DocumentInfo> entry : files.entrySet()) {
                if (isNameVariant(entry.getKey(), name)) {
                    candidates.add(entry.getValue());
                }
            }
            boolean[] readable = new boolean[candidates.size()];
            boolean[] directory = new boolean[candidates.size()];
            long[] sizes = new long[candidates.size()];
            for (int i = 0; i < candidates.size(); i++) {
                DocumentInfo candidate = candidates.get(i);
                readable[i] = canRead(candidate.uri);
                directory[i] = candidate.directory;
                sizes[i] = ReceivedFileMatcher.reportedSize(candidate.sizeKnown, candidate.size);
            }
            int selected = ReceivedFileMatcher.firstAvailableIndex(readable, directory, sizes, expectedSize);
            return selected < 0 ? null : candidates.get(selected);
        }

        boolean canRead(Uri uri) {
            if (uri == null) {
                return false;
            }
            try (AssetFileDescriptor descriptor = resolver.openAssetFileDescriptor(uri, "r")) {
                return descriptor != null;
            } catch (Exception error) {
                return false;
            }
        }

        private long queryModifiedMs(Uri uri) {
            // MediaStore is not a DocumentsProvider: querying last_modified throws
            // across Binder for every received file on Android 10/Huawei devices.
            if (MediaStore.AUTHORITY.equals(uri.getAuthority())) {
                long seconds = queryLongColumn(uri, MediaStore.MediaColumns.DATE_MODIFIED);
                return seconds > 0 ? seconds * 1000L : 0;
            }
            long documentMs = queryLongColumn(uri, DocumentsContract.Document.COLUMN_LAST_MODIFIED);
            if (documentMs > 0) {
                return documentMs;
            }
            long mediaSeconds = queryLongColumn(uri, MediaStore.MediaColumns.DATE_MODIFIED);
            return mediaSeconds > 0 ? mediaSeconds * 1000L : 0;
        }

        private long queryLongColumn(Uri uri, String column) {
            try {
                Cursor cursor = resolver.query(uri, new String[]{column}, null, null, null);
                if (cursor == null) {
                    return 0;
                }
                try {
                    if (cursor.moveToFirst() && !cursor.isNull(0)) {
                        return cursor.getLong(0);
                    }
                } finally {
                    cursor.close();
                }
            } catch (Exception ignored) {
            }
            return 0;
        }

        synchronized DocumentInfo ensureFile(String remotePath, String fallbackName, boolean replaceExisting, Session session) throws Exception {
            checkStopped(session);
            String[] parts = pathParts(remotePath);
            String destination = parts.length == 0 ? safeName(fallbackName) : String.join("/", parts);
            // Conservative for providers backed by case-insensitive storage.
            claims.claimPath(destination.toLowerCase(Locale.ROOT), remotePath);
            DocumentInfo result = createOrFindFile(remotePath, fallbackName, replaceExisting);
            claims.claimUri(result.uri.toString(), remotePath);
            return result;
        }

        private void checkUnclaimed(DocumentInfo target, String remotePath) throws Exception {
            if (target != null) claims.checkUri(target.uri.toString(), remotePath);
        }

        private DocumentInfo createOrFindFile(String remotePath, String fallbackName, boolean replaceExisting) throws Exception {
            if (!replaceExisting) {
                DocumentInfo remembered = rememberedTarget(remotePath);
                if (remembered != null && !remembered.directory) {
                    checkUnclaimed(remembered, remotePath);
                    return remembered;
                }
            }
            String[] parts = pathParts(remotePath);
            String name = parts.length == 0 ? safeName(fallbackName) : parts[parts.length - 1];
            if (treeUri == null) {
                return ensurePublicFile(parts, name, remotePath, replaceExisting);
            }
            Uri parent = ensureTreeDirectory(parentPath(parts));
            Map<String, DocumentInfo> files = treeChildren(parent);
            DocumentInfo existing = files.get(name);
            if (existing != null && !existing.directory) {
                checkUnclaimed(existing, remotePath);
                if (!replaceExisting) {
                    rememberTarget(remotePath, existing.uri);
                    return existing;
                }
                if (DocumentsContract.deleteDocument(resolver, existing.uri)) files.remove(name);
            }
            String mimeType = mimeTypeForSave(name, MimeTypeMap.getSingleton()::getMimeTypeFromExtension);
            return createNamedTarget(name, remotePath, files,
                    candidate -> DocumentsContract.createDocument(resolver, parent, mimeType, candidate));
        }

        private DocumentInfo createNamedTarget(String name, String remotePath,
                                               Map<String, DocumentInfo> files, CreateTarget create) throws Exception {
            int rejected = 0;
            for (int number = 0; ; number++) {
                String candidate = numberedFileName(name, number);
                if (files.containsKey(candidate)) continue;
                Uri uri = create.create(candidate);
                if (uri == null) throw new IOException("Cannot create " + candidate);

                DocumentInfo info = new DocumentInfo(uri, 0);
                checkUnclaimed(info, remotePath);
                String actual = receivedFileName(resolver, uri);
                DocumentInfo existing = files.get(actual);
                if (existing == null && acceptsCreatedName(name, candidate, actual)) {
                    files.put(actual, info);
                    rememberTarget(remotePath, uri);
                    return info;
                }
                if (existing != null && uri.equals(existing.uri)) {
                    throw new IOException("Storage returned an existing file for " + candidate);
                }
                // This URI was just created and has not been written or recorded.
                boolean removed = treeUri != null
                        ? DocumentsContract.deleteDocument(resolver, uri)
                        : resolver.delete(uri, null, null) > 0;
                if (!removed || ++rejected >= MAX_PROVIDER_NAME_RETRIES) {
                    throw new IOException("Cannot save with a unique name that preserves the extension: " + name);
                }
            }
        }

        synchronized Uri ensureDirectory(String remotePath) throws Exception {
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
                    DocumentsContract.Document.COLUMN_SIZE,
                    DocumentsContract.Document.COLUMN_LAST_MODIFIED,
                    DocumentsContract.Document.COLUMN_MIME_TYPE
            };
            Cursor cursor = resolver.query(children, columns, null, null, null);
            if (cursor != null) {
                try {
                    while (cursor.moveToNext()) {
                        String childName = cursor.getString(1);
                        if (childName == null) {
                            continue;
                        }
                        boolean sizeKnown = !cursor.isNull(2);
                        long size = sizeKnown ? cursor.getLong(2) : 0;
                        long modifiedMs = cursor.isNull(3) ? 0 : cursor.getLong(3);
                        Uri childUri = DocumentsContract.buildDocumentUriUsingTree(treeUri, cursor.getString(0));
                        boolean directory = DocumentsContract.Document.MIME_TYPE_DIR.equals(cursor.getString(4));
                        childrenMap.put(childName, new DocumentInfo(childUri, size, modifiedMs, directory, sizeKnown, childName));
                    }
                } finally {
                    cursor.close();
                }
            }
            treeChildrenCache.put(parentId, childrenMap);
            return childrenMap;
        }

        private DocumentInfo ensurePublicFile(String[] parts, String name, String remotePath, boolean replaceExisting) throws Exception {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                String relativeDir = publicRelativeDir(parts);
                Map<String, DocumentInfo> files = publicFiles(relativeDir);
                if (replaceExisting) {
                    DocumentInfo existing = files.get(name);
                    if (existing != null) {
                        checkUnclaimed(existing, remotePath);
                        if (resolver.delete(existing.uri, null, null) > 0) files.remove(name);
                    }
                } else {
                    // When no saved URI is available, resume from a download we can see by name.
                    DocumentInfo variant = bestResumeVariant(files, name);
                    if (variant != null) {
                        checkUnclaimed(variant, remotePath);
                        rememberTarget(remotePath, variant.uri);
                        return variant;
                    }
                }
                String mimeType = mimeTypeForSave(name, MimeTypeMap.getSingleton()::getMimeTypeFromExtension);
                return createNamedTarget(name, remotePath, files, candidate -> {
                    ContentValues values = new ContentValues();
                    values.put(MediaStore.Downloads.DISPLAY_NAME, candidate);
                    values.put(MediaStore.Downloads.MIME_TYPE, mimeType);
                    values.put(MediaStore.Downloads.RELATIVE_PATH, relativeDir);
                    return resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values);
                });
            }

            File dir = publicDownloadDir(parts);
            if (!dir.exists() && !dir.mkdirs()) {
                throw new IllegalStateException("Cannot create " + dir.getAbsolutePath());
            }
            File file = new File(dir, name);
            if (file.isFile()) {
                Uri uri = Uri.fromFile(file);
                DocumentInfo existing = new DocumentInfo(uri, file.length(), file.lastModified(), false);
                checkUnclaimed(existing, remotePath);
                if (!replaceExisting) {
                    rememberTarget(remotePath, uri);
                    return existing;
                }
                if (!file.delete()) {
                    throw new IllegalStateException("Cannot replace " + file.getAbsolutePath());
                }
            }
            for (int number = 0; ; number++) {
                File candidate = new File(dir, numberedFileName(name, number));
                if (candidate.createNewFile()) {
                    Uri uri = Uri.fromFile(candidate);
                    rememberTarget(remotePath, uri);
                    return new DocumentInfo(uri, 0);
                }
            }
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
                    MediaStore.Downloads.SIZE,
                    MediaStore.MediaColumns.DATE_MODIFIED
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
                        boolean sizeKnown = !cursor.isNull(2);
                        long size = sizeKnown ? cursor.getLong(2) : 0;
                        long modifiedMs = cursor.isNull(3) ? 0 : cursor.getLong(3) * 1000L;
                        Uri uri = Uri.withAppendedPath(MediaStore.Downloads.EXTERNAL_CONTENT_URI, String.valueOf(id));
                        files.put(name, new DocumentInfo(uri, size, modifiedMs, false, sizeKnown, name));
                    }
                } finally {
                    cursor.close();
                }
            }
            publicFilesCache.put(normalizedDir, files);
            return files;
        }

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
        private final Set<HttpURLConnection> activeConnections = new HashSet<>();
        private ExecutorService executor;
        final ReceiveTimings timings = new ReceiveTimings();
        private boolean terminated;
        private Runnable terminationListener;

        void stop() {
            List<HttpURLConnection> connections;
            ExecutorService pool;
            synchronized (this) {
                stopped = true;
                connections = new ArrayList<>(activeConnections);
                activeConnections.clear();
                pool = executor;
            }
            if (pool != null) pool.shutdownNow();
            for (HttpURLConnection conn : connections) disconnect(conn);
        }

        boolean isStopped() {
            return stopped;
        }

        void onTerminated(Runnable listener) {
            boolean runNow;
            synchronized (this) {
                if (!terminated) {
                    terminationListener = listener;
                    return;
                }
                runNow = true;
            }
            if (runNow && listener != null) {
                listener.run();
            }
        }

        void markTerminated() {
            Runnable listener;
            synchronized (this) {
                if (terminated) {
                    return;
                }
                terminated = true;
                listener = terminationListener;
                terminationListener = null;
            }
            if (listener != null) {
                listener.run();
            }
        }

        void attach(HttpURLConnection conn) {
            if (conn == null) return;
            synchronized (this) {
                if (!stopped) {
                    activeConnections.add(conn);
                    return;
                }
            }
            disconnect(conn);
        }

        void detach(HttpURLConnection conn) {
            synchronized (this) {
                activeConnections.remove(conn);
            }
            if (conn != null) disconnect(conn);
        }

        void attachExecutor(ExecutorService pool) {
            synchronized (this) {
                executor = pool;
                if (!stopped) return;
            }
            pool.shutdownNow();
        }

        synchronized void detachExecutor(ExecutorService pool) {
            if (executor == pool) executor = null;
        }

        private static void disconnect(HttpURLConnection conn) {
            try { conn.disconnect(); } catch (RuntimeException ignored) { }
        }
    }

    private static final class ProgressClock {
        long networkBytes;
        long logicalBytes;
        private final long startMs = System.currentTimeMillis();
        private long lastMs = startMs;
        private long lastBytes;
        private double bytesPerSecond;

        boolean shouldEmit(long doneBytes) {
            long now = System.currentTimeMillis();
            long elapsed = now - lastMs;
            if (elapsed < 200 && doneBytes != lastBytes) {
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
        String linkTarget = "";
        String linkError;
        String name;
        boolean isDir;
        long size;
        String path;
        long modifiedMs; // epoch millis of last modification; 0 when unknown
    }

    static final class ReceivedTarget {
        final Uri uri;
        final String displayName;
        final long size;
        final long modifiedMs;

        ReceivedTarget(Uri uri, String displayName, long size, long modifiedMs) {
            this.uri = uri;
            this.displayName = displayName;
            this.size = size;
            this.modifiedMs = modifiedMs;
        }
    }

    static final class DownloadFailure {
        final String path;
        final String message;

        DownloadFailure(String path, String message) {
            this.path = path == null || path.trim().isEmpty() ? "(unknown)" : path;
            this.message = message == null || message.trim().isEmpty() ? "unknown error" : message;
        }
    }

    private static final class ListResult {
        final List<RemoteFile> files;
        final boolean missing;

        ListResult(List<RemoteFile> files, boolean missing) {
            this.files = files;
            this.missing = missing;
        }
    }

    private static final class RepairManifest {
        String path;
        long size;
        long modifiedMs;
        long blockSize;
        long manifestSize;
        long limitSize;
        final List<ManifestBlock> blocks = new ArrayList<>();

        boolean isFull() {
            return manifestSize <= 0 || manifestSize >= size;
        }
    }

    private static final class ManifestBlock {
        final int index;
        final long offset;
        final long size;
        final String hash;

        ManifestBlock(int index, long offset, long size, String hash) {
            this.index = index;
            this.offset = offset;
            this.size = size;
            this.hash = hash == null ? "" : hash;
        }
    }

    private static final class RepairRange {
        final long offset;
        long size;

        RepairRange(long offset, long size) {
            this.offset = offset;
            this.size = size;
        }

        long endInclusive() {
            return offset + size - 1;
        }
    }

    private static final class RepairPlan {
        final List<RepairRange> ranges;
        final long transferBytes;
        final long dirtyBytes;
        final int dirtyRangeCount;
        final int kind;

        RepairPlan(List<RepairRange> ranges, long transferBytes, long dirtyBytes, int dirtyRangeCount, int kind) {
            this.ranges = ranges;
            this.transferBytes = transferBytes;
            this.dirtyBytes = dirtyBytes;
            this.dirtyRangeCount = dirtyRangeCount;
            this.kind = kind;
        }
    }

    private static final class DocumentInfo {
        final Uri uri;
        final long size;
        final long modifiedMs;
        final boolean directory;
        final boolean sizeKnown;
        final String displayName;

        DocumentInfo(Uri uri, long size) {
            this(uri, size, 0, false, true);
        }

        DocumentInfo(Uri uri, long size, long modifiedMs) {
            this(uri, size, modifiedMs, false, true);
        }

        DocumentInfo(Uri uri, long size, long modifiedMs, boolean directory) {
            this(uri, size, modifiedMs, directory, true);
        }

        DocumentInfo(Uri uri, long size, long modifiedMs, boolean directory, boolean sizeKnown) {
            this(uri, size, modifiedMs, directory, sizeKnown, null);
        }

        DocumentInfo(Uri uri, long size, long modifiedMs, boolean directory, boolean sizeKnown, String displayName) {
            this.uri = uri;
            this.size = size;
            this.modifiedMs = modifiedMs;
            this.directory = directory;
            this.sizeKnown = sizeKnown;
            this.displayName = displayName;
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
