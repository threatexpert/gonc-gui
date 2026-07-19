package cn.threatexpert.gonc;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import static org.junit.Assert.assertTrue;

public class ReceiveControllerBrowseRefreshTest {
    @Test
    public void enteringDirectoryStartsReceivedTargetRefresh() throws Exception {
        Path source = Paths.get("src/main/java/cn/threatexpert/gonc/ReceiveController.java");
        String text = new String(Files.readAllBytes(source), StandardCharsets.UTF_8);
        int start = text.indexOf("private void browseRemotePath(String path)");
        int end = text.indexOf("private List<String> currentDownloadPaths()", start);
        assertTrue("browseRemotePath method must exist", start >= 0 && end > start);
        String method = text.substring(start, end);
        assertTrue("directory entry must refresh received targets",
                method.contains("refreshReceivedTargets(receiveRunId, remoteCurrentPath)"));
    }
}
