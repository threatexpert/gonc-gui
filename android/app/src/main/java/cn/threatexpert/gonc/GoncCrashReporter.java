package cn.threatexpert.gonc;

import android.content.Context;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.PrintWriter;
import java.io.RandomAccessFile;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

final class GoncCrashReporter {
    private static final String DIAGNOSTICS_FILE = "gonc_diagnostics.log";
    private static final String LAST_CRASH_FILE = "gonc_last_crash.txt";
    private static final String LAST_STAGE_FILE = "gonc_last_stage.txt";
    private static final int MAX_DIAGNOSTICS_CHARS = 24000;
    private static final int MAX_DIAGNOSTICS_LINES = 240;
    private static final int MAX_ACTIVITY_LOG_LINES_IN_REPORT = 120;
    private static final long MAX_DIAGNOSTICS_AGE_MS = 24L * 60L * 60L * 1000L;

    private static final Object LOCK = new Object();
    private static boolean installed;
    private static Context appContext;
    private static FileOutputStream diagnosticsOut;
    private static RandomAccessFile crashStore;
    private static RandomAccessFile stageStore;
    private static String lastCrash = "";
    private static String lastStage = "";
    private static final List<String> diagnostics = new ArrayList<>();

    private GoncCrashReporter() {
    }

    static void install(Context context) {
        Context application = context.getApplicationContext();
        synchronized (LOCK) {
            appContext = application;
            if (installed) {
                if (diagnosticsOut == null || crashStore == null || stageStore == null) {
                    prepareStoresLocked(application);
                }
                return;
            }
            prepareStoresLocked(application);
            installed = true;
        }
        Thread.UncaughtExceptionHandler previous = Thread.getDefaultUncaughtExceptionHandler();
        Thread.setDefaultUncaughtExceptionHandler((thread, error) -> {
            recordCrash(application, thread, error);
            if (previous != null) {
                previous.uncaughtException(thread, error);
            }
        });
    }

    static void stage(Context context, String stage) {
        ensureReady(context);
        synchronized (LOCK) {
            lastStage = stage == null ? "" : stage;
            writeRandomLocked(stageStore, lastStage);
        }
        appendLog(context, "stage", stage);
    }

    static void appendLog(Context context, String level, String message) {
        if (message == null || message.trim().isEmpty()) {
            return;
        }
        ensureReady(context);
        String line = System.currentTimeMillis()
                + " "
                + (level == null ? "info" : level)
                + " "
                + message
                + "\n";
        synchronized (LOCK) {
            diagnostics.add(line.substring(0, line.length() - 1));
            trimDiagnosticsListLocked();
            if (diagnosticsOut != null) {
                try {
                    diagnosticsOut.write(line.getBytes(StandardCharsets.UTF_8));
                    diagnosticsOut.flush();
                } catch (IOException ignored) {
                }
            }
        }
    }

    static void recordNonFatal(Context context, String message, Throwable error) {
        ensureReady(context);
        StringBuilder builder = new StringBuilder();
        builder.append("Non-fatal VPN error");
        if (message != null && !message.trim().isEmpty()) {
            builder.append(": ").append(message.trim());
        }
        builder.append('\n').append("Stage: ").append(lastStage(context)).append('\n');
        if (error != null) {
            builder.append(stackTrace(error));
        }
        synchronized (LOCK) {
            lastCrash = builder.toString();
            writeRandomLocked(crashStore, lastCrash);
        }
        appendLog(context, "error", builder.toString());
    }

    static String lastCrash(Context context) {
        ensureReady(context);
        synchronized (LOCK) {
            return lastCrash == null ? "" : lastCrash;
        }
    }

    static void clearLastCrash(Context context) {
        ensureReady(context);
        synchronized (LOCK) {
            lastCrash = "";
            writeRandomLocked(crashStore, "");
        }
    }

    static void clearDiagnostics(Context context) {
        ensureReady(context);
        synchronized (LOCK) {
            diagnostics.clear();
            lastStage = "";
            writeRandomLocked(stageStore, "");
            if (diagnosticsOut != null) {
                try {
                    diagnosticsOut.getChannel().truncate(0);
                    diagnosticsOut.flush();
                } catch (IOException ignored) {
                }
            }
        }
    }

    static String diagnostics(Context context, List<String> activityLogs) {
        ensureReady(context);
        String stage;
        String crash;
        List<String> persistent;
        synchronized (LOCK) {
            stage = lastStage == null ? "" : lastStage;
            crash = lastCrash == null ? "" : lastCrash;
            persistent = new ArrayList<>(diagnostics);
        }

        StringBuilder builder = new StringBuilder();
        builder.append("Gonc Android diagnostics\n");
        builder.append("Stage: ").append(stage).append('\n');
        builder.append("Last crash:\n");
        builder.append(crash.trim().isEmpty() ? "-" : crash).append('\n');
        builder.append("\nPersistent diagnostics:\n");
        if (persistent.isEmpty()) {
            builder.append("-");
        } else {
            for (String line : persistent) {
                builder.append(line).append('\n');
            }
        }
        builder.append('\n');
        builder.append("\nCurrent activity log:\n");
        if (activityLogs == null || activityLogs.isEmpty()) {
            builder.append("-");
        } else {
            int start = Math.max(0, activityLogs.size() - MAX_ACTIVITY_LOG_LINES_IN_REPORT);
            for (int i = start; i < activityLogs.size(); i++) {
                String line = activityLogs.get(i);
                builder.append(line).append('\n');
            }
        }
        return builder.toString();
    }

    private static void ensureReady(Context context) {
        synchronized (LOCK) {
            if (appContext == null && context != null) {
                appContext = context.getApplicationContext();
            }
            if (appContext != null && diagnosticsOut == null) {
                prepareStoresLocked(appContext);
            }
        }
    }

    private static void prepareStoresLocked(Context context) {
        File dir = context.getFilesDir();
        File diagnosticsFile = new File(dir, DIAGNOSTICS_FILE);
        File crashFile = new File(dir, LAST_CRASH_FILE);
        File stageFile = new File(dir, LAST_STAGE_FILE);
        try {
            String trimmed = trimDiagnostics(readText(diagnosticsFile));
            writeText(diagnosticsFile, trimmed);
            diagnostics.clear();
            for (String line : trimmed.split("\n")) {
                if (!line.trim().isEmpty()) {
                    diagnostics.add(line);
                }
            }
            diagnosticsOut = new FileOutputStream(diagnosticsFile, true);
        } catch (IOException ignored) {
        }
        try {
            lastCrash = readText(crashFile);
            crashStore = new RandomAccessFile(crashFile, "rw");
        } catch (IOException ignored) {
        }
        try {
            lastStage = readText(stageFile).trim();
            stageStore = new RandomAccessFile(stageFile, "rw");
        } catch (IOException ignored) {
        }
    }

    private static String trimDiagnostics(String raw) {
        if (raw == null || raw.isEmpty()) {
            return "";
        }
        String[] lines = raw.split("\n");
        long cutoff = System.currentTimeMillis() - MAX_DIAGNOSTICS_AGE_MS;
        List<String> kept = new ArrayList<>();
        for (String line : lines) {
            if (line == null || line.trim().isEmpty()) {
                continue;
            }
            if (kept.size() >= MAX_DIAGNOSTICS_LINES) {
                kept.remove(0);
            }
            long timestamp = parseTimestamp(line);
            if (timestamp <= 0 || timestamp >= cutoff) {
                kept.add(line);
            }
        }
        return joinTrimmed(kept);
    }

    private static void trimDiagnosticsListLocked() {
        long cutoff = System.currentTimeMillis() - MAX_DIAGNOSTICS_AGE_MS;
        for (int i = diagnostics.size() - 1; i >= 0; i--) {
            long timestamp = parseTimestamp(diagnostics.get(i));
            if (timestamp > 0 && timestamp < cutoff) {
                diagnostics.remove(i);
            }
        }
        while (diagnostics.size() > MAX_DIAGNOSTICS_LINES) {
            diagnostics.remove(0);
        }
        while (joinTrimmed(diagnostics).length() > MAX_DIAGNOSTICS_CHARS && diagnostics.size() > 1) {
            diagnostics.remove(0);
        }
    }

    private static String joinTrimmed(List<String> lines) {
        StringBuilder builder = new StringBuilder();
        int start = 0;
        while (start < lines.size()) {
            builder.setLength(0);
            for (int i = start; i < lines.size(); i++) {
                builder.append(lines.get(i)).append('\n');
            }
            if (builder.length() <= MAX_DIAGNOSTICS_CHARS || start == lines.size() - 1) {
                return builder.length() <= MAX_DIAGNOSTICS_CHARS
                        ? builder.toString()
                        : builder.substring(builder.length() - MAX_DIAGNOSTICS_CHARS);
            }
            start++;
        }
        return "";
    }

    private static long parseTimestamp(String line) {
        int space = line.indexOf(' ');
        if (space <= 0) {
            return -1L;
        }
        try {
            return Long.parseLong(line.substring(0, space));
        } catch (NumberFormatException ignored) {
            return -1L;
        }
    }

    private static void recordCrash(Context context, Thread thread, Throwable error) {
        ensureReady(context);
        StringBuilder builder = new StringBuilder();
        builder.append("Thread: ").append(thread == null ? "-" : thread.getName()).append('\n');
        builder.append("Stage: ").append(lastStage(context)).append('\n');
        builder.append(stackTrace(error));
        synchronized (LOCK) {
            lastCrash = builder.toString();
            writeRandomLocked(crashStore, lastCrash);
        }
        appendLog(context, "fatal", builder.toString());
    }

    private static String lastStage(Context context) {
        ensureReady(context);
        synchronized (LOCK) {
            return lastStage == null ? "" : lastStage;
        }
    }

    private static String stackTrace(Throwable error) {
        if (error == null) {
            return "";
        }
        StringWriter out = new StringWriter();
        error.printStackTrace(new PrintWriter(out));
        return out.toString();
    }

    private static String readText(File file) throws IOException {
        if (file == null || !file.exists()) {
            return "";
        }
        FileInputStream in = new FileInputStream(file);
        try {
            byte[] data = new byte[(int) Math.min(file.length(), MAX_DIAGNOSTICS_CHARS * 2L)];
            int read = in.read(data);
            if (read <= 0) {
                return "";
            }
            return new String(data, 0, read, StandardCharsets.UTF_8);
        } finally {
            try {
                in.close();
            } catch (IOException ignored) {
            }
        }
    }

    private static void writeText(File file, String value) throws IOException {
        FileOutputStream out = new FileOutputStream(file, false);
        try {
            out.write((value == null ? "" : value).getBytes(StandardCharsets.UTF_8));
            out.flush();
        } finally {
            try {
                out.close();
            } catch (IOException ignored) {
            }
        }
    }

    private static void writeRandomLocked(RandomAccessFile file, String value) {
        if (file == null) {
            return;
        }
        try {
            file.seek(0);
            file.setLength(0);
            file.write((value == null ? "" : value).getBytes(StandardCharsets.UTF_8));
        } catch (IOException ignored) {
        }
    }
}
