package cn.threatexpert.gonc;

import java.util.function.Consumer;

/** Dialog-local edits are committed at most once, only for editable callers. */
final class PassphraseQrDraft {
    private final String initial;
    private final boolean editable;
    private String value;
    private boolean closed;

    PassphraseQrDraft(String initial, boolean editable) {
        this.initial = initial.trim();
        this.value = this.initial;
        this.editable = editable;
    }
    String value() { return value; }
    void edit(String text) { if (editable && !closed) value = text.trim(); }
    void close(Consumer<String> onChanged) {
        if (closed) return;
        closed = true;
        if (editable && !initial.equals(value) && onChanged != null) onChanged.accept(value);
    }
}
