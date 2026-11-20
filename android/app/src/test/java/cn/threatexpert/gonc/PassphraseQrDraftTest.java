package cn.threatexpert.gonc;

import org.junit.Test;
import java.util.ArrayList;
import java.util.List;
import static org.junit.Assert.*;

public class PassphraseQrDraftTest {
    @Test public void readOnlyCallerCannotChangePassphrase() {
        PassphraseQrDraft draft = new PassphraseQrDraft("original123", false);
        draft.edit("changed123");
        assertEquals("original123", draft.value());
        draft.close(value -> fail("read-only callback"));
    }
    @Test public void editingChangesQrValueAndReportsFinalValueOnlyOnceOnClose() {
        PassphraseQrDraft draft = new PassphraseQrDraft("original123", true);
        List<String> changes = new ArrayList<>();
        draft.edit("first123");
        draft.edit(" final123 ");
        assertEquals("final123", draft.value());
        assertTrue(changes.isEmpty());
        draft.close(changes::add);
        draft.close(changes::add);
        draft.edit("late1234");
        assertEquals("final123", draft.value());
        assertEquals(1, changes.size());
        assertEquals("final123", changes.get(0));
    }
    @Test public void unchangedValuesDoNotNotifyAndClearingReturnsEmpty() {
        PassphraseQrDraft unchanged = new PassphraseQrDraft("original123", true);
        unchanged.edit(" original123 ");
        unchanged.close(value -> fail("unchanged callback"));
        PassphraseQrDraft cleared = new PassphraseQrDraft("original123", true);
        cleared.edit("  ");
        List<String> changes = new ArrayList<>();
        cleared.close(changes::add);
        assertEquals(1, changes.size());
        assertEquals("", changes.get(0));
    }
}
