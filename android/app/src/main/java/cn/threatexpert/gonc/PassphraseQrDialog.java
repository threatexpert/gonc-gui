package cn.threatexpert.gonc;

import android.app.Activity;
import android.app.Dialog;
import android.graphics.Color;
import android.graphics.Typeface;
import android.text.Editable;
import android.text.TextWatcher;
import android.view.Gravity;
import android.view.View;
import android.view.Window;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import java.util.function.Consumer;
import java.util.function.Function;

final class PassphraseQrDialog {
    static void show(Activity activity, UiKit ui, String passphrase, boolean editable,
                     int titleRes, Function<String, String> encode,
                     Consumer<String> onChanged, Consumer<String> copy) {
        PassphraseQrDraft draft = new PassphraseQrDraft(passphrase, editable);
        final int qrSize = ui.dp(Math.min(260, Math.max(120, activity.getResources().getConfiguration().screenWidthDp - 72)));
        Dialog dialog = new Dialog(activity);
        dialog.requestWindowFeature(Window.FEATURE_NO_TITLE);
        LinearLayout box = ui.column();
        box.setPadding(ui.dp(18), ui.dp(18), ui.dp(18), ui.dp(18));
        box.setBackground(ui.rounded(Color.WHITE, ui.dp(8), 0, 0));
        TextView title = ui.text(activity.getString(titleRes), 16, ui.ink(), Typeface.BOLD);
        title.setGravity(Gravity.CENTER);
        box.addView(title);

        ImageView image = new ImageView(activity);
        image.setAdjustViewBounds(true);
        image.setBackgroundColor(Color.WHITE);
        image.setContentDescription(activity.getString(titleRes));
        image.setOnLongClickListener(v -> {
            Object encoded = image.getTag();
            if (encoded instanceof String) copy.accept((String) encoded);
            return true;
        });
        LinearLayout.LayoutParams imageParams = new LinearLayout.LayoutParams(qrSize, qrSize);
        imageParams.gravity = Gravity.CENTER_HORIZONTAL;
        imageParams.topMargin = ui.dp(12);
        box.addView(image, imageParams);

        TextView hint = ui.text(activity.getString(R.string.passphrase_qr_copy_hint), 12, ui.muted(), Typeface.NORMAL);
        hint.setGravity(Gravity.CENTER);
        box.addView(hint, ui.blockParams(ui.dp(4)));
        TextView error = ui.text("", 12, Color.rgb(180, 40, 40), Typeface.NORMAL);
        error.setVisibility(View.GONE);
        box.addView(error, ui.blockParams(ui.dp(4)));

        Runnable refresh = () -> {
            image.setImageDrawable(null);
            image.setTag(null);
            image.setLongClickable(false);
            error.setVisibility(View.GONE);
            if (draft.value().isEmpty()) return;
            try {
                image.setImageBitmap(QrCodes.encode(encode.apply(draft.value()), qrSize));
                image.setTag(draft.value());
                image.setLongClickable(true);
            } catch (Exception failure) {
                error.setText(R.string.toast_qr_failed);
                error.setVisibility(View.VISIBLE);
            }
        };

        TextView value;
        if (editable) {
            EditText editor = new EditText(activity);
            editor.setSingleLine(true);
            editor.setHint(R.string.passphrase_input_hint);
            editor.setInputType(android.text.InputType.TYPE_CLASS_TEXT | android.text.InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD);
            editor.setText(draft.value());
            editor.addTextChangedListener(new TextWatcher() {
                public void beforeTextChanged(CharSequence s, int start, int count, int after) {}
                public void onTextChanged(CharSequence s, int start, int before, int count) {
                    draft.edit(s.toString());
                    image.removeCallbacks(refresh);
                    image.setImageDrawable(null);
                    image.setTag(null);
                    image.setLongClickable(false);
                    image.postDelayed(refresh, 180);
                }
                public void afterTextChanged(Editable s) {}
            });
            value = editor;
        } else {
            value = ui.text(draft.value(), 14, ui.ink(), Typeface.BOLD);
            value.setTextIsSelectable(true);
        }
        value.setTextColor(ui.ink());
        value.setTextSize(14);
        value.setContentDescription(activity.getString(R.string.passphrase));
        value.setTypeface(Typeface.MONOSPACE);
        value.setGravity(editable ? Gravity.START | Gravity.CENTER_VERTICAL : Gravity.CENTER);
        value.setPadding(ui.dp(10), ui.dp(8), ui.dp(10), ui.dp(8));
        if (editable) {
            TextView label = ui.text(activity.getString(R.string.editable_passphrase), 12, ui.muted(), Typeface.NORMAL);
            box.addView(label, ui.blockParams(ui.dp(8)));
            value.setMinimumHeight(ui.dp(48));
            android.graphics.drawable.StateListDrawable background = new android.graphics.drawable.StateListDrawable();
            background.addState(new int[]{android.R.attr.state_focused}, ui.rounded(Color.WHITE, ui.dp(6), Color.rgb(37, 99, 235), 2));
            background.addState(new int[]{}, ui.rounded(Color.WHITE, ui.dp(6), Color.rgb(148, 163, 184), 1));
            value.setBackgroundTintList(null);
            value.setBackground(background);
        } else {
            value.setBackground(ui.rounded(Color.rgb(248, 251, 255), ui.dp(7), Color.rgb(226, 232, 240), 1));
        }
        if (editable) {
            EditText editor = (EditText) value;
            editor.setPadding(ui.dp(10), ui.dp(8), ui.dp(48), ui.dp(8));
            FrameLayout field = new FrameLayout(activity);
            field.addView(editor, new FrameLayout.LayoutParams(
                    android.view.ViewGroup.LayoutParams.MATCH_PARENT, android.view.ViewGroup.LayoutParams.WRAP_CONTENT));
            View random = ui.iconButton(R.drawable.ic_key_random, R.string.random_passphrase, () -> {
                editor.setText(Passwords.generate());
                editor.setSelection(editor.getText().length());
            });
            field.addView(random, new FrameLayout.LayoutParams(ui.dp(44), ui.dp(48), Gravity.END | Gravity.CENTER_VERTICAL));
            box.addView(field, ui.blockParams(ui.dp(8)));
        } else {
            box.addView(value, ui.blockParams(ui.dp(8)));
        }
        Button close = ui.secondaryButton(activity.getString(R.string.close));
        close.setOnClickListener(v -> dialog.dismiss());
        box.addView(close, ui.blockParams(ui.dp(12)));
        ScrollView scroll = new ScrollView(activity);
        scroll.addView(box);
        dialog.setContentView(scroll);
        dialog.setOnDismissListener(ignored -> { image.removeCallbacks(refresh); draft.close(onChanged); });
        if (dialog.getWindow() != null) dialog.getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_HIDDEN | WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        refresh.run();
        dialog.show();
    }
}
