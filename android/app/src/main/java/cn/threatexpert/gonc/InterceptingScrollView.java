package cn.threatexpert.gonc;

import android.content.Context;
import android.util.AttributeSet;
import android.view.MotionEvent;
import android.view.ViewParent;
import android.widget.ScrollView;

final class InterceptingScrollView extends ScrollView {
    InterceptingScrollView(Context context) {
        super(context);
    }

    public InterceptingScrollView(Context context, AttributeSet attrs) {
        super(context, attrs);
    }

    public InterceptingScrollView(Context context, AttributeSet attrs, int defStyleAttr) {
        super(context, attrs, defStyleAttr);
    }

    @Override
    public boolean dispatchTouchEvent(MotionEvent event) {
        ViewParent parent = getParent();
        if (parent != null) {
            int action = event.getActionMasked();
            parent.requestDisallowInterceptTouchEvent(action == MotionEvent.ACTION_DOWN || action == MotionEvent.ACTION_MOVE);
        }
        return super.dispatchTouchEvent(event);
    }
}
