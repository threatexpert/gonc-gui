package cn.threatexpert.gonc;

import android.content.Context;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.TextView;

import java.util.Locale;

/**
 * Stateless UI builder + formatting vocabulary shared by every screen module.
 *
 * <p>Holds only a {@link Context}; it never owns module state. Extracted from
 * {@code MainActivity} so per-module panels can build views with the same look
 * without duplicating primitives. MainActivity keeps thin delegates to these
 * methods during the migration; those delegates go away once a panel's view
 * code moves into its own class and calls {@code ui.*} directly.
 */
final class UiKit {
    private final Context context;

    UiKit(Context context) {
        this.context = context;
    }

    // --- colors -----------------------------------------------------------

    int ink() {
        return Color.rgb(23, 32, 51);
    }

    int muted() {
        return Color.rgb(100, 116, 139);
    }

    // --- metrics ----------------------------------------------------------

    int dp(int value) {
        return (int) (value * context.getResources().getDisplayMetrics().density + 0.5f);
    }

    // --- drawables --------------------------------------------------------

    GradientDrawable rounded(int color, int radius, int strokeColor, int strokeWidthDp) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(color);
        drawable.setCornerRadius(radius);
        if (strokeWidthDp > 0) {
            drawable.setStroke(dp(strokeWidthDp), strokeColor);
        }
        return drawable;
    }

    // --- containers -------------------------------------------------------

    LinearLayout card() {
        LinearLayout card = column();
        card.setPadding(dp(16), dp(16), dp(16), dp(16));
        card.setBackground(rounded(Color.WHITE, dp(8), Color.rgb(216, 226, 238), 1));
        card.setLayoutParams(blockParams(dp(12)));
        return card;
    }

    LinearLayout row() {
        LinearLayout layout = new LinearLayout(context);
        layout.setOrientation(LinearLayout.HORIZONTAL);
        return layout;
    }

    LinearLayout column() {
        LinearLayout layout = new LinearLayout(context);
        layout.setOrientation(LinearLayout.VERTICAL);
        return layout;
    }

    // --- layout params ----------------------------------------------------

    LinearLayout.LayoutParams blockParams() {
        return blockParams(dp(10));
    }

    LinearLayout.LayoutParams blockParams(int topMargin) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
        );
        params.setMargins(0, topMargin, 0, 0);
        return params;
    }

    LinearLayout.LayoutParams dividerParams(int topMargin) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                dp(1)
        );
        params.setMargins(0, topMargin, 0, 0);
        return params;
    }

    LinearLayout.LayoutParams actionParams() {
        return actionParams(dp(40));
    }

    LinearLayout.LayoutParams actionParams(int height) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, height, 1);
        params.setMargins(dp(8), 0, 0, 0);
        return params;
    }

    LinearLayout.LayoutParams metricParams() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1);
        params.setMargins(dp(8), 0, 0, 0);
        return params;
    }

    // --- text -------------------------------------------------------------

    TextView text(String value, int sp, int color, int style) {
        TextView view = new TextView(context);
        view.setText(value);
        view.setTextSize(sp);
        view.setTextColor(color);
        view.setTypeface(Typeface.DEFAULT, style);
        return view;
    }

    TextView sectionTitle(String title) {
        TextView view = text(title, 16, ink(), Typeface.BOLD);
        view.setPadding(0, 0, 0, dp(10));
        return view;
    }

    View sectionBoundaryTitle(String title, boolean separated) {
        LinearLayout box = column();
        if (separated) {
            box.addView(sectionDivider(), new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(1)));
        }
        TextView view = text(title, 13, Color.rgb(64, 81, 105), Typeface.BOLD);
        view.setPadding(0, separated ? dp(12) : 0, 0, dp(2));
        box.addView(view);
        return box;
    }

    View sectionDivider() {
        View line = new View(context);
        line.setBackgroundColor(Color.rgb(226, 232, 240));
        return line;
    }

    // --- buttons ----------------------------------------------------------

    Button button(String label, int color, int background) {
        Button button = new Button(context);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(14);
        button.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        button.setTextColor(color);
        button.setBackground(rounded(background, dp(6), 0, 0));
        return button;
    }

    Button modeButton(String label, boolean active) {
        Button button = new Button(context);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(14);
        button.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        button.setTextColor(active ? Color.WHITE : Color.rgb(82, 101, 125));
        button.setBackground(rounded(active ? ink() : Color.TRANSPARENT, dp(6), 0, 0));
        return button;
    }

    Button primaryButton(String label) {
        Button button = button(label, Color.WHITE, Color.rgb(40, 112, 216));
        button.setTextSize(16);
        return button;
    }

    Button outlineButton(String label) {
        Button button = new Button(context);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(16);
        button.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        button.setTextColor(Color.rgb(40, 112, 216));
        button.setBackground(rounded(Color.TRANSPARENT, dp(6), Color.rgb(40, 112, 216), 1));
        return button;
    }

    Button dangerButton(String label) {
        Button button = button(label, Color.WHITE, Color.rgb(201, 63, 63));
        button.setTextSize(16);
        return button;
    }

    Button warningButton(String label) {
        Button button = button(label, Color.WHITE, Color.rgb(217, 119, 6));
        button.setTextSize(16);
        return button;
    }

    Button secondaryButton(String label) {
        return button(label, ink(), Color.rgb(237, 242, 247));
    }

    Button ghostButton(String label) {
        return button(label, Color.rgb(40, 112, 216), Color.TRANSPARENT);
    }

    Button compactGhostButton(String label) {
        Button button = ghostButton(label);
        button.setTextSize(13);
        button.setMinWidth(0);
        button.setMinimumWidth(0);
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        button.setPadding(dp(8), 0, dp(8), 0);
        button.setIncludeFontPadding(false);
        return button;
    }

    Button quietTouchButton(String label) {
        Button button = ghostButton(label);
        button.setMinWidth(dp(56));
        button.setMinimumWidth(dp(56));
        button.setMinHeight(dp(44));
        button.setMinimumHeight(dp(44));
        button.setPadding(dp(8), 0, dp(8), 0);
        return button;
    }

    Button segmentedButton(String label, boolean active) {
        Button button = button(label, active ? Color.rgb(40, 112, 216) : Color.rgb(82, 101, 125), active ? Color.rgb(232, 240, 252) : Color.TRANSPARENT);
        button.setTextSize(12);
        button.setPadding(dp(6), 0, dp(6), 0);
        button.setMinHeight(0);
        button.setMinimumHeight(0);
        return button;
    }

    void setControlEnabled(View view, boolean enabled) {
        view.setEnabled(enabled);
        view.setAlpha(enabled ? 1.0f : 0.45f);
    }

    // --- formatting (pure) ------------------------------------------------

    String formatBytes(long value) {
        if (value < 0) {
            return context.getString(R.string.unknown_size);
        }
        String[] units = {"B", "KB", "MB", "GB", "TB"};
        double size = value;
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            size /= 1024;
            unit++;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    String formatRate(double bytesPerSecond) {
        if (bytesPerSecond <= 0) {
            return "0 B/s";
        }
        String[] units = {"B/s", "KB/s", "MB/s", "GB/s"};
        double size = bytesPerSecond;
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            size /= 1024;
            unit++;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    String formatPercent(long done, long total) {
        if (total <= 0) {
            return "0%";
        }
        double value = Math.min(100, Math.max(0, done * 100.0 / total));
        return String.format(Locale.ROOT, "%.1f%%", value);
    }

    int progressValue(long done, long total) {
        if (total <= 0) {
            return 0;
        }
        return (int) Math.min(1000, Math.max(0, done * 1000.0 / total));
    }

    String formatDuration(long seconds) {
        long clean = Math.max(0, seconds);
        long hours = clean / 3600;
        long minutes = (clean % 3600) / 60;
        long secs = clean % 60;
        if (hours > 0) {
            return String.format(Locale.ROOT, "%dh %02dm", hours, minutes);
        }
        if (minutes > 0) {
            return String.format(Locale.ROOT, "%dm %02ds", minutes, secs);
        }
        return String.format(Locale.ROOT, "%ds", secs);
    }

    String emptyDash(String value) {
        return value == null || value.trim().isEmpty() ? "-" : value.trim();
    }

    String titleCase(String value) {
        if (value == null || value.trim().isEmpty()) {
            return "-";
        }
        String clean = value.trim();
        return clean.substring(0, 1).toUpperCase(Locale.ROOT) + clean.substring(1);
    }

    // --- status/route display (shared by VPN client + receive) ------------

    String normalizeMetricStatus(String status) {
        if (status == null || status.trim().isEmpty()) {
            return "-";
        }
        return status.trim().toLowerCase(Locale.ROOT);
    }

    String routeLabel(String mode) {
        if (mode == null) {
            return "";
        }
        String clean = mode.trim();
        if (clean.isEmpty() || "-".equals(clean)) {
            return "";
        }
        if ("relay".equals(clean.toLowerCase(Locale.ROOT))) {
            return context.getString(R.string.route_relay);
        }
        if ("p2p".equals(clean.toLowerCase(Locale.ROOT))) {
            return context.getString(R.string.route_direct);
        }
        return "";
    }

    String appendRoute(String label, String route) {
        return route.isEmpty() ? label : label + " · " + route;
    }
}
