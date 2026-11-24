package cn.threatexpert.gonc;

import android.animation.ValueAnimator;
import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.view.View;
import android.view.animation.LinearInterpolator;

/**
 * A dot with rings that slowly spread from it, to show that something is
 * still going on, such as Nearby looking for devices or announcing this one.
 * It animates only while attached to a window, and follows the system
 * animation scale: with animations off it draws the dot alone.
 */
final class PulseView extends View {
    private static final long PERIOD_MS = 2400;

    private final Paint dot = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint ring = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final int color;
    private ValueAnimator animator;
    private float phase;

    PulseView(Context context, int color) {
        super(context);
        this.color = color;
        dot.setColor(color);
        ring.setStyle(Paint.Style.STROKE);
        ring.setStrokeWidth(context.getResources().getDisplayMetrics().density * 1.5f);
    }

    @Override
    protected void onAttachedToWindow() {
        super.onAttachedToWindow();
        if (!ValueAnimator.areAnimatorsEnabled()) {
            return;
        }
        animator = ValueAnimator.ofFloat(0f, 1f);
        animator.setDuration(PERIOD_MS);
        animator.setRepeatCount(ValueAnimator.INFINITE);
        animator.setInterpolator(new LinearInterpolator());
        animator.addUpdateListener(a -> {
            phase = (float) a.getAnimatedValue();
            invalidate();
        });
        animator.start();
    }

    @Override
    protected void onDetachedFromWindow() {
        if (animator != null) {
            animator.cancel();
            animator = null;
        }
        super.onDetachedFromWindow();
    }

    @Override
    protected void onDraw(Canvas canvas) {
        super.onDraw(canvas);
        float cx = getWidth() / 2f;
        float cy = getHeight() / 2f;
        float max = Math.min(cx, cy) - ring.getStrokeWidth();
        float core = max * 0.38f;
        if (animator != null) {
            // Two rings half a period apart, each growing and fading out.
            for (float offset : new float[]{0f, 0.5f}) {
                float p = (phase + offset) % 1f;
                ring.setColor(color);
                ring.setAlpha((int) (150 * (1f - p)));
                canvas.drawCircle(cx, cy, core + (max - core) * p, ring);
            }
        }
        canvas.drawCircle(cx, cy, core, dot);
    }
}
