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
        startActivity(context, buildIntent(context,
                openPlan(uri.toString(), mime, chooser), uri, target.displayName,
                R.string.open_with));
    }

    static void share(Context context, HttpReceiver.ReceivedTarget target) {
        Uri uri = shareableUri(context, target.uri);
        String mime = mimeType(context, uri, target.displayName);
        startActivity(context, buildIntent(context,
                sharePlan(uri.toString(), mime), uri, target.displayName,
                R.string.share_file));
    }

    static void showInfo(
            Context context, HttpReceiver.ReceivedTarget target, String saveLocationLabel) {
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
                R.string.file_info_save_location, saveLocationLabel));

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
            if (!isCanonicalChild(canonicalDownloads, canonicalFile)) {
                throw new IllegalArgumentException("Legacy received file is outside Download/Gonc");
            }
            return FileProvider.getUriForFile(
                    context, context.getPackageName() + FILE_PROVIDER_SUFFIX, canonicalFile);
        } catch (IOException error) {
            throw new IllegalArgumentException("Unable to resolve legacy received file", error);
        }
    }

    static boolean isCanonicalChild(File root, File candidate) throws IOException {
        String rootPath = root.getCanonicalFile().getPath();
        String candidatePath = candidate.getCanonicalFile().getPath();
        return candidatePath.startsWith(rootPath + File.separator);
    }

    static IntentPlan openPlan(String uri, String mimeType, boolean chooser) {
        return new IntentPlan(Intent.ACTION_VIEW, uri, mimeType, chooser,
                true, true, false);
    }

    static IntentPlan sharePlan(String uri, String mimeType) {
        return new IntentPlan(Intent.ACTION_SEND, uri, mimeType, true,
                true, true, true);
    }

    private static Intent buildIntent(
            Context context,
            IntentPlan plan,
            Uri uri,
            String displayName,
            int chooserTitleRes) {
        Intent target = new Intent(plan.action);
        if (plan.attachStream) {
            target.setType(plan.mimeType).putExtra(Intent.EXTRA_STREAM, uri);
        } else {
            target.setDataAndType(uri, plan.mimeType);
        }
        applyUriAccess(context, target, uri, displayName, plan);
        if (!plan.chooser) {
            return target;
        }
        Intent chooser = Intent.createChooser(target, context.getString(chooserTitleRes));
        applyUriAccess(context, chooser, uri, displayName, plan);
        return chooser;
    }

    static final class IntentPlan {
        final String action;
        final String uri;
        final String mimeType;
        final boolean chooser;
        final boolean grantReadUriPermission;
        final boolean attachClipData;
        final boolean attachStream;

        IntentPlan(
                String action,
                String uri,
                String mimeType,
                boolean chooser,
                boolean grantReadUriPermission,
                boolean attachClipData,
                boolean attachStream) {
            this.action = action;
            this.uri = uri;
            this.mimeType = mimeType;
            this.chooser = chooser;
            this.grantReadUriPermission = grantReadUriPermission;
            this.attachClipData = attachClipData;
            this.attachStream = attachStream;
        }
    }

    private static void applyUriAccess(
            Context context,
            Intent intent,
            Uri uri,
            String displayName,
            IntentPlan plan) {
        if (plan.attachClipData) {
            intent.setClipData(ClipData.newUri(
                    context.getContentResolver(), displayName, uri));
        }
        if (plan.grantReadUriPermission) {
            intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        }
    }

    private static void startActivity(Context context, Intent intent) {
        if (!(context instanceof Activity)) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
        }
        context.startActivity(intent);
    }
}
