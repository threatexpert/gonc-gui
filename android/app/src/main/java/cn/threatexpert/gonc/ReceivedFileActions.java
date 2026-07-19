package cn.threatexpert.gonc;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Environment;
import android.text.format.DateFormat;
import android.text.format.Formatter;

import androidx.core.content.FileProvider;

import java.io.File;
import java.io.IOException;
import java.net.URLConnection;
import java.util.Date;
import java.util.Locale;

final class ReceivedFileActions {
    private static final String FILE_PROVIDER_SUFFIX = ".received-files";
    private static final String BINARY_MIME_TYPE = "application/octet-stream";
    private static final String APK_MIME_TYPE = "application/vnd.android.package-archive";

    private ReceivedFileActions() {
    }

    static String fallbackMimeType(String name) {
        String lower = name == null ? "" : name.toLowerCase(Locale.ROOT);
        if (lower.endsWith(".apk")) {
            return APK_MIME_TYPE;
        }
        String guessed = URLConnection.guessContentTypeFromName(lower);
        return guessed == null ? BINARY_MIME_TYPE : guessed;
    }

    static void open(Context context, HttpReceiver.ReceivedTarget target) {
        open(context, target, false);
    }

    static void openWith(Context context, HttpReceiver.ReceivedTarget target) {
        open(context, target, true);
    }

    static void open(Context context, HttpReceiver.ReceivedTarget target, boolean chooser) {
        Uri uri = shareableUri(context, target.uri);
        String mime = mimeType(context, uri, target.displayName);
        Intent view = grantReadAccess(context,
                new Intent(Intent.ACTION_VIEW).setDataAndType(uri, mime), uri, target.displayName);
        startActivity(context, chooser
                ? chooser(context, view, uri, target.displayName, R.string.open_with)
                : view);
    }

    static void share(Context context, HttpReceiver.ReceivedTarget target) {
        Uri uri = shareableUri(context, target.uri);
        String mime = mimeType(context, uri, target.displayName);
        Intent send = grantReadAccess(context, new Intent(Intent.ACTION_SEND)
                .setType(mime)
                .putExtra(Intent.EXTRA_STREAM, uri), uri, target.displayName);
        startActivity(context, chooser(context, send, uri, target.displayName, R.string.share_file));
    }

    static void showInfo(Context context, HttpReceiver.ReceivedTarget target) {
        StringBuilder message = new StringBuilder()
                .append(context.getString(R.string.file_info_name, target.displayName))
                .append('\n')
                .append(context.getString(R.string.file_info_size,
                        Formatter.formatFileSize(context, Math.max(0L, target.size))));
        if (target.modifiedMs > 0L) {
            Date modified = new Date(target.modifiedMs);
            String formatted = DateFormat.getMediumDateFormat(context).format(modified)
                    + " " + DateFormat.getTimeFormat(context).format(modified);
            message.append('\n').append(context.getString(R.string.file_info_modified, formatted));
        }
        message.append('\n').append(context.getString(
                R.string.file_info_save_location, context.getString(R.string.received)));

        new AlertDialog.Builder(context)
                .setTitle(R.string.file_information)
                .setMessage(message)
                .setPositiveButton(R.string.close, null)
                .show();
    }

    private static String mimeType(Context context, Uri uri, String displayName) {
        String resolved = context.getContentResolver().getType(uri);
        if (resolved == null || resolved.isEmpty() || BINARY_MIME_TYPE.equals(resolved)) {
            return fallbackMimeType(displayName);
        }
        return resolved;
    }

    private static Uri shareableUri(Context context, Uri uri) {
        if (!"file".equalsIgnoreCase(uri.getScheme())) {
            return uri;
        }

        File file = new File(uri.getPath());
        File downloads = new File(
                Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), "Gonc");
        try {
            File canonicalFile = file.getCanonicalFile();
            File canonicalDownloads = downloads.getCanonicalFile();
            String rootPath = canonicalDownloads.getPath();
            String filePath = canonicalFile.getPath();
            if (!filePath.startsWith(rootPath + File.separator)) {
                throw new IllegalArgumentException("Legacy received file is outside Download/Gonc");
            }
            return FileProvider.getUriForFile(
                    context, context.getPackageName() + FILE_PROVIDER_SUFFIX, canonicalFile);
        } catch (IOException error) {
            throw new IllegalArgumentException("Unable to resolve legacy received file", error);
        }
    }

    private static Intent grantReadAccess(Context context, Intent intent, Uri uri, String displayName) {
        intent.setClipData(ClipData.newUri(context.getContentResolver(), displayName, uri));
        return intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
    }

    private static Intent chooser(
            Context context, Intent target, Uri uri, String displayName, int titleRes) {
        Intent chooser = Intent.createChooser(target, context.getString(titleRes));
        return grantReadAccess(context, chooser, uri, displayName);
    }

    private static void startActivity(Context context, Intent intent) {
        if (!(context instanceof Activity)) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
        }
        context.startActivity(intent);
    }
}
