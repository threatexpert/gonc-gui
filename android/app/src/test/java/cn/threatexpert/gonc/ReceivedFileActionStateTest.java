package cn.threatexpert.gonc;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class ReceivedFileActionStateTest {
    @Test
    public void keepsMarkerButDisablesActionsDuringDownload() {
        assertTrue(ReceivedFileActionState.markerVisible(true));
        assertFalse(ReceivedFileActionState.actionsEnabled(true, true));
    }

    @Test
    public void enablesAvailableTargetOnlyWhileIdle() {
        assertTrue(ReceivedFileActionState.actionsEnabled(true, false));
        assertFalse(ReceivedFileActionState.actionsEnabled(false, false));
    }
}
