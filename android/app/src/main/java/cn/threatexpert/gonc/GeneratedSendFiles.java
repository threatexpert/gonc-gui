package cn.threatexpert.gonc;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStreamWriter;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.TimeZone;

final class GeneratedSendFiles {
    interface TokenSource {
        String next();
    }

    private static final SecureRandom RANDOM = new SecureRandom();

    private GeneratedSendFiles() {
    }

    static File createText(File root, String source, String text) throws IOException {
        return createText(root, source, text, System.currentTimeMillis(),
                TimeZone.getDefault(), GeneratedSendFiles::randomToken);
    }

    static File copyImage(File root, String source, String extension, InputStream input)
            throws IOException {
        return copyImage(root, source, extension, input, System.currentTimeMillis(),
                TimeZone.getDefault(), GeneratedSendFiles::randomToken);
    }

    static File createText(File root, String source, String text, long now,
                           TimeZone zone, TokenSource tokens) throws IOException {
        File target = allocate(root, source, "txt", now, zone, tokens);
        boolean written = false;
        try (FileOutputStream stream = new FileOutputStream(target);
             OutputStreamWriter writer = new OutputStreamWriter(stream, StandardCharsets.UTF_8)) {
            writer.write(text);
            writer.flush();
            stream.getFD().sync();
            written = true;
            return target;
        } finally {
            if (!written) {
                target.delete();
            }
        }
    }

    static File copyImage(File root, String source, String extension, InputStream input,
                          long now, TimeZone zone, TokenSource tokens) throws IOException {
        File target = allocate(root, source, safeExtension(extension), now, zone, tokens);
        boolean written = false;
        try (InputStream sourceStream = input;
             FileOutputStream output = new FileOutputStream(target)) {
            byte[] buffer = new byte[128 * 1024];
            int count;
            while ((count = sourceStream.read(buffer)) >= 0) {
                output.write(buffer, 0, count);
            }
            output.getFD().sync();
            written = true;
            return target;
        } finally {
            if (!written) {
                target.delete();
            }
        }
    }

    static String extensionForMime(String mimeType) {
        if (mimeType == null) {
            return "bin";
        }
        switch (mimeType.toLowerCase(Locale.US)) {
            case "image/png":
                return "png";
            case "image/jpeg":
            case "image/jpg":
                return "jpg";
            case "image/webp":
                return "webp";
            case "image/gif":
                return "gif";
            case "image/heic":
            case "image/heif":
                return "heic";
            default:
                return "bin";
        }
    }

    static boolean deleteOwned(File root, File candidate) {
        if (root == null || candidate == null) {
            return false;
        }
        try {
            File canonicalRoot = root.getCanonicalFile();
            File canonicalCandidate = candidate.getCanonicalFile();
            if (!canonicalRoot.equals(canonicalCandidate.getParentFile())) {
                return false;
            }
            return !canonicalCandidate.exists() || canonicalCandidate.delete();
        } catch (IOException ignored) {
            return false;
        }
    }

    private static File allocate(File root, String source, String extension, long now,
                                 TimeZone zone, TokenSource tokens) throws IOException {
        if (!root.exists() && !root.mkdirs()) {
            throw new IOException("cannot create generated send directory: " + root);
        }
        if (!root.isDirectory()) {
            throw new IOException("generated send path is not a directory: " + root);
        }
        SimpleDateFormat format = new SimpleDateFormat("yyyyMMdd-HHmmss", Locale.US);
        format.setTimeZone(zone);
        String timestamp = format.format(new Date(now));
        String prefix = safeSource(source);
        for (int attempt = 0; attempt < 100; attempt++) {
            String token = safeToken(tokens.next());
            File candidate = new File(root,
                    prefix + "-" + timestamp + "-" + token + "." + safeExtension(extension));
            if (candidate.createNewFile()) {
                return candidate;
            }
        }
        throw new IOException("cannot allocate a unique generated send file");
    }

    private static String safeSource(String source) {
        String value = source == null ? "content" : source.toLowerCase(Locale.US)
                .replaceAll("[^a-z0-9-]", "-")
                .replaceAll("-+", "-")
                .replaceAll("^-|-$", "");
        return value.isEmpty() ? "content" : value;
    }

    private static String safeExtension(String extension) {
        String value = extension == null ? "" : extension.toLowerCase(Locale.US)
                .replaceAll("[^a-z0-9]", "");
        return value.isEmpty() ? "bin" : value;
    }

    private static String safeToken(String token) {
        String value = token == null ? "" : token.toLowerCase(Locale.US)
                .replaceAll("[^a-f0-9]", "");
        return value.isEmpty() ? randomToken() : value;
    }

    private static String randomToken() {
        return String.format(Locale.US, "%04x", RANDOM.nextInt(0x10000));
    }
}
