package cn.threatexpert.gonc;

import android.graphics.Typeface;
import android.os.Handler;
import android.os.SystemClock;
import android.view.Choreographer;
import android.view.Gravity;
import android.view.View;
import android.widget.TextView;

import java.lang.ref.WeakReference;
import java.util.function.Consumer;

/** One automatic passphrase generation per module, starting with its first visible page. */
final class InitialPassphrase {
    private static final long DISPLAY_MS = 1000;

    private enum State { NOT_SHOWN, GENERATING, READY }

    private State state = State.NOT_SHOWN;
    private int generation;
    private WeakReference<View> currentPage = new WeakReference<>(null);

    boolean isPending() {
        return state != State.READY;
    }

    void reset() {
        generation++;
        state = State.NOT_SHOWN;
        currentPage.clear();
    }

    void show(View page, Handler handler, Consumer<String> onReady, Runnable render) {
        if (!isPending()) return;
        currentPage = new WeakReference<>(page);
        if (state != State.NOT_SHOWN) return;

        int request = generation;
        page.post(() -> Choreographer.getInstance().postFrameCallback(ignored -> {
            if (request != generation || state != State.NOT_SHOWN
                    || page != currentPage.get() || !page.isAttachedToWindow()) return;
            state = State.GENERATING;
            long showQrAt = SystemClock.uptimeMillis() + DISPLAY_MS;
            String password = Passwords.generate();
            handler.postAtTime(() -> {
                if (request != generation) return;
                onReady.accept(password);
                state = State.READY;
                View visiblePage = currentPage.get();
                if (visiblePage != null && visiblePage.isAttachedToWindow()) {
                    render.run();
                }
            }, showQrAt);
        }));
    }

    static TextView generatingMessage(ModuleHost host) {
        UiKit u = host.ui();
        TextView text = u.text(host.context().getString(R.string.generating_passphrase), 12, u.muted(), Typeface.NORMAL);
        text.setGravity(Gravity.CENTER);
        return text;
    }
}
