package cn.threatexpert.gonc;

import android.content.ContentResolver;
import android.content.Context;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

final class FileCache {
    interface Progress {
        void onCopied(int done, int total, ShareItem item, long bytes);
    }

    private FileCache() {
    }

    static void prepareSharedFiles(Context context, List<ShareItem> items, Progress progress) throws IOException {
        File dir = new File(context.getCacheDir(), "shared-input");
        ensureCleanDirectory(dir);

        ContentResolver resolver = context.getContentResolver();
        Set<String> names = new HashSet<>();
        int total = items.size();
        for (int i = 0; i < items.size(); i++) {
            ShareItem item = items.get(i);
            if ("file".equalsIgnoreCase(item.uri().getScheme())) {
                File direct = new File(item.uri().getPath());
                if (direct.isFile()) {
                    item.setCachedFile(direct);
                    if (progress != null) {
                        progress.onCopied(i + 1, total, item, 0);
                    }
                    continue;
                }
            }
            String fileName = uniqueName(sanitize(item.displayName()), names);
            File target = new File(dir, fileName);
            long copied = copyUri(resolver, item, target);
            item.setCachedFile(target);
            if (progress != null) {
                progress.onCopied(i + 1, total, item, copied);
            }
        }
    }

    private static long copyUri(ContentResolver resolver, ShareItem item, File target) throws IOException {
        InputStream in = resolver.openInputStream(item.uri());
        if (in == null) {
            throw new IOException("cannot open shared file: " + item.displayName());
        }
        try (InputStream input = in; FileOutputStream output = new FileOutputStream(target)) {
            byte[] buffer = new byte[128 * 1024];
            long copied = 0;
            int n;
            while ((n = input.read(buffer)) >= 0) {
                output.write(buffer, 0, n);
                copied += n;
            }
            output.getFD().sync();
            return copied;
        }
    }

    private static void ensureCleanDirectory(File dir) throws IOException {
        if (dir.exists()) {
            deleteChildren(dir);
        } else if (!dir.mkdirs()) {
            throw new IOException("cannot create cache directory: " + dir.getAbsolutePath());
        }
    }

    private static void deleteChildren(File dir) throws IOException {
        File[] children = dir.listFiles();
        if (children == null) {
            return;
        }
        for (File child : children) {
            if (child.isDirectory()) {
                deleteChildren(child);
            }
            if (!child.delete()) {
                throw new IOException("cannot clean cache file: " + child.getAbsolutePath());
            }
        }
    }

    private static String sanitize(String name) {
        String cleaned = name.replaceAll("[\\\\/:*?\"<>|\\p{Cntrl}]", "_").trim();
        if (cleaned.isEmpty() || ".".equals(cleaned) || "..".equals(cleaned)) {
            return "shared-file";
        }
        return cleaned;
    }

    private static String uniqueName(String base, Set<String> names) {
        if (names.add(base)) {
            return base;
        }
        int dot = base.lastIndexOf('.');
        String stem = dot > 0 ? base.substring(0, dot) : base;
        String ext = dot > 0 ? base.substring(dot) : "";
        for (int i = 2; ; i++) {
            String candidate = stem + "-" + i + ext;
            if (names.add(candidate)) {
                return candidate;
            }
        }
    }
}
