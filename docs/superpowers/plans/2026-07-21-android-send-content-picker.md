# Android Send Content Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Android's permanent file/folder buttons with a clickable send list and add-type dialog supporting files, folders, image/video media, authored text, and text/image clipboard content.

**Architecture:** Keep `SendController` as the share-list and live-session owner, and keep `MainActivity` as the Android intent/clipboard host. Add focused helpers for owned generated files and bounded asynchronous thumbnails; extend `ShareItem` with explicit owned-file metadata so cleanup can never delete user content.

**Tech Stack:** Android Java, minSdk 26, compileSdk/targetSdk 35, platform `AlertDialog`/SAF/photo-picker APIs, AndroidX Core 1.13.1, JUnit 4, Gradle.

## Global Constraints

- Modify only `gonc-gui/android`; do not modify `gonetcat` or rebuild `mobilegonc.aar`.
- Media means images and videos only; audio is excluded.
- Clipboard means text and images only; video, audio, and arbitrary clipboard files are excluded.
- Empty Chinese copy is exactly `点击这里添加文件、文件夹、媒体或文字`.
- Populated-list add copy is exactly `点击这里继续添加`.
- The add dialog title is exactly `想加入什么内容？`, with choices in this order: 文件、文件夹、媒体、文字、剪贴板.
- Running send sessions continue to permit add, clear-all, and per-item removal through the existing live `updateShareItems` behavior.
- Generated files use UTF-8 for text, collision-resistant names, explicit ownership metadata, and deletion only inside the app-owned generated-send directory.
- Do not add a custom media browser or broad media/storage permission.

## File Structure

- Create `android/app/src/main/java/cn/threatexpert/gonc/GeneratedSendFiles.java`: exclusive generated-name allocation, UTF-8 writes, image copies, MIME extension mapping, and safe owned-file deletion.
- Create `android/app/src/main/java/cn/threatexpert/gonc/SendThumbnailLoader.java`: bounded asynchronous image/video thumbnails and stale-render rejection.
- Modify `android/app/src/main/java/cn/threatexpert/gonc/ShareItem.java`: explicit nullable owned generated file.
- Modify `android/app/src/main/java/cn/threatexpert/gonc/AndroidFileSource.java`: open app-owned `file://` inputs directly.
- Modify `android/app/src/main/java/cn/threatexpert/gonc/ModuleHost.java`: media picker, clipboard import, and reveal-after-render seams.
- Modify `android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java`: Android picker intents/results, clipboard extraction, generated-item creation, partial failures, and outer-scroll reveal.
- Modify `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`: approved list UI, dialogs, mutations, cleanup, icons, and post-add reveal.
- Modify `android/app/src/main/java/cn/threatexpert/gonc/UiKit.java`: compact circular removal control and icon/text picker-row primitives only where reused.
- Modify `android/app/src/main/res/values/strings.xml` and `android/app/src/main/res/values-zh/strings.xml`: exact localized copy and errors.
- Create vector resources `android/app/src/main/res/drawable/ic_send_file.xml`, `ic_send_folder.xml`, `ic_send_media.xml`, `ic_send_text.xml`, `ic_send_clipboard.xml`, and `ic_send_play.xml`.
- Create tests `GeneratedSendFilesTest.java`, `AndroidSendImportContractTest.java`, `AndroidSendListContractTest.java`, and `SendThumbnailLoaderTest.java` under `android/app/src/test/java/cn/threatexpert/gonc/`.

---

### Task 1: Owned Generated Files and Direct File Reading

**Files:**
- Create: `android/app/src/main/java/cn/threatexpert/gonc/GeneratedSendFiles.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ShareItem.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/AndroidFileSource.java:331-351`
- Test: `android/app/src/test/java/cn/threatexpert/gonc/GeneratedSendFilesTest.java`

**Interfaces:**
- Consumes: Java `File`, `InputStream`, UTF-8, a caller-provided timestamp, timezone, and token source.
- Produces: `GeneratedSendFiles.createText(File,String,String)`, `GeneratedSendFiles.copyImage(File,String,String,InputStream)`, `GeneratedSendFiles.extensionForMime(String)`, `GeneratedSendFiles.deleteOwned(File,File)`, and `ShareItem.ownedFile()`.

- [ ] **Step 1: Write failing pure-Java tests for names, UTF-8, collision retry, MIME extensions, and safe deletion**

```java
public class GeneratedSendFilesTest {
    @Rule public TemporaryFolder temp = new TemporaryFolder();

    @Test public void textNamesAreUniqueAndUtf8() throws Exception {
        File root = temp.newFolder("generated-send");
        Queue<String> tokens = new ArrayDeque<>(Arrays.asList("7f3a", "7f3a", "a92c"));
        GeneratedSendFiles.TokenSource source = tokens::remove;
        TimeZone utc = TimeZone.getTimeZone("UTC");

        File first = GeneratedSendFiles.createText(root, "text", "你好", 1784648730000L, utc, source);
        File second = GeneratedSendFiles.createText(root, "text", "again", 1784648730000L, utc, source);

        assertEquals("text-20260721-154530-7f3a.txt", first.getName());
        assertEquals("text-20260721-154530-a92c.txt", second.getName());
        assertEquals("你好", new String(Files.readAllBytes(first.toPath()), StandardCharsets.UTF_8));
    }

    @Test public void mimeExtensionAndDeletionAreConstrained() throws Exception {
        File root = temp.newFolder("generated-send");
        File owned = new File(root, "clipboard-image.png");
        assertTrue(owned.createNewFile());
        File outside = temp.newFile("user.png");

        assertEquals("png", GeneratedSendFiles.extensionForMime("image/png"));
        assertEquals("jpg", GeneratedSendFiles.extensionForMime("image/jpeg"));
        assertEquals("bin", GeneratedSendFiles.extensionForMime("image/x-unknown"));
        assertTrue(GeneratedSendFiles.deleteOwned(root, owned));
        assertFalse(GeneratedSendFiles.deleteOwned(root, outside));
        assertTrue(outside.exists());
    }
}
```

- [ ] **Step 2: Run the focused test and verify the expected red state**

Run from `android`:

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.GeneratedSendFilesTest
```

Expected: compilation fails because `GeneratedSendFiles` and `ShareItem.ownedFile()` do not exist.

- [ ] **Step 3: Implement exclusive generated files and explicit ownership**

Implement this public surface in package-private `GeneratedSendFiles`:

```java
final class GeneratedSendFiles {
    interface TokenSource { String next(); }

    static File createText(File root, String source, String text) throws IOException;
    static File copyImage(File root, String source, String extension,
                          InputStream input) throws IOException;

    static File createText(File root, String source, String text, long now,
                           TimeZone zone, TokenSource tokens) throws IOException;

    static File copyImage(File root, String source, String extension, InputStream input,
                          long now, TimeZone zone, TokenSource tokens) throws IOException;

    static String extensionForMime(String mimeType);
    static boolean deleteOwned(File root, File candidate);
}
```

The short production overloads use `System.currentTimeMillis()`, `TimeZone.getDefault()`, and a four-hex-character token from `SecureRandom`; the injectable overloads provide deterministic tests. For both creation methods, call `root.mkdirs()` when absent, format `yyyyMMdd-HHmmss`, sanitize `source` to `[a-z0-9-]`, and loop until `File.createNewFile()` succeeds. Write text with `OutputStreamWriter(..., StandardCharsets.UTF_8)` and copy images with a 128 KiB buffer. Resolve both root and candidate with `getCanonicalFile()` before deletion and require the candidate parent to be the canonical root.

Extend `ShareItem` without changing existing call sites:

```java
private final File ownedFile;

ShareItem(Uri uri, String displayName, long size, String mimeType,
          boolean directory, boolean treeUri, long lastModifiedMillis, File ownedFile) {
    // assign all existing fields and ownedFile
}

File ownedFile() { return ownedFile; }
```

Existing constructors delegate with `ownedFile = null`. In `AndroidFileSource.OpenHandle.open(...)`, handle app-owned file URIs before `ContentResolver`:

```java
if ("file".equalsIgnoreCase(uri.getScheme())) {
    FileInputStream input = new FileInputStream(new File(uri.getPath()));
    return new OpenHandle(input, input.getChannel(), null);
}
```

- [ ] **Step 4: Run focused and bridge regression tests**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.GeneratedSendFilesTest --tests cn.threatexpert.gonc.MobileGoncBridgeTest
```

Expected: PASS; user-selected items still have `ownedFile() == null`, generated files are readable through the file-source path.

- [ ] **Step 5: Commit Task 1**

```powershell
git add android/app/src/main/java/cn/threatexpert/gonc/GeneratedSendFiles.java android/app/src/main/java/cn/threatexpert/gonc/ShareItem.java android/app/src/main/java/cn/threatexpert/gonc/AndroidFileSource.java android/app/src/test/java/cn/threatexpert/gonc/GeneratedSendFilesTest.java
git commit -m "feat(android): own generated send files"
```

---

### Task 2: File, Folder, Media, Text, and Clipboard Import Plumbing

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ModuleHost.java:35-45`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java:70-85,239-275,827-842,1650-1875`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/src/main/res/values-zh/strings.xml`
- Test: `android/app/src/test/java/cn/threatexpert/gonc/AndroidSendImportContractTest.java`

**Interfaces:**
- Consumes: Task 1 `GeneratedSendFiles` and owned-file `ShareItem` constructor.
- Produces: `ModuleHost.pickSendMedia()`, `ModuleHost.importSendClipboard()`, `ModuleHost.addAuthoredSendText(String)`, and `MainActivity` results that call `SendController.addFiles(...)`.

- [ ] **Step 1: Write failing import-contract tests**

Create a source/resource test that reads `ModuleHost.java`, `MainActivity.java`, and both string files. Assert these exact contracts:

```java
@Test public void hostExposesAllSendImportEntries() throws Exception {
    String host = source("ModuleHost.java");
    assertTrue(host.contains("void pickSendMedia();"));
    assertTrue(host.contains("void importSendClipboard();"));
    assertTrue(host.contains("void addAuthoredSendText(String text);"));
}

@Test public void mediaUsesPhotoPickerWithImageVideoSafFallback() throws Exception {
    String activity = source("MainActivity.java");
    assertTrue(activity.contains("REQUEST_OPEN_SEND_MEDIA"));
    assertTrue(activity.contains("MediaStore.ACTION_PICK_IMAGES"));
    assertTrue(activity.contains("Intent.EXTRA_MIME_TYPES"));
    assertTrue(activity.contains("\"image/*\", \"video/*\""));
}

@Test public void clipboardAcceptsImageBeforeTextAndRejectsOtherKinds() throws Exception {
    String activity = source("MainActivity.java");
    assertTrue(activity.indexOf("clipboardImageUri") < activity.indexOf("coerceToText"));
    assertTrue(zhStrings().contains("剪贴板中没有可添加的文字或图片"));
}
```

- [ ] **Step 2: Run the import contract test and verify it fails**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidSendImportContractTest
```

Expected: FAIL because media/text/clipboard host methods, request code, and strings are absent.

- [ ] **Step 3: Add host methods, media picker, and robust picker-result URI collection**

Add request code `REQUEST_OPEN_SEND_MEDIA = 1009`. Implement `openSendMediaPicker()` using `MediaStore.ACTION_PICK_IMAGES` on API 33+ with multiple selection and the platform maximum; use `ACTION_OPEN_DOCUMENT`, `EXTRA_ALLOW_MULTIPLE`, and `EXTRA_MIME_TYPES = {"image/*", "video/*"}` below API 33.

Do not reuse incoming-share action filtering for picker results. Add:

```java
private List<Uri> collectPickerUris(Intent data) {
    Map<String, Uri> result = new LinkedHashMap<>();
    putUri(result, data.getData());
    ClipData clips = data.getClipData();
    if (clips != null) {
        for (int i = 0; i < clips.getItemCount(); i++) {
            putUri(result, clips.getItemAt(i).getUri());
        }
    }
    return new ArrayList<>(result.values());
}
```

Use it for file and media activity results. For media, reject a returned MIME type unless it starts with `image/` or `video/`; append readable items and toast a localized failed count for rejected/unreadable entries. Cancellation returns without a toast.

- [ ] **Step 4: Add authored-text and clipboard generated items**

Use one generated root:

```java
private File generatedSendRoot() {
    return new File(getCacheDir(), "generated-send");
}
```

For authored text, reject only the empty string, call `GeneratedSendFiles.createText(generatedSendRoot(), "text", text)`, build a `ShareItem(Uri.fromFile(file), file.getName(), file.length(), "text/plain", false, false, file.lastModified(), file)`, and pass it to `sendController.addFiles(...)`.

For clipboard, inspect all `ClipData.Item`s for an image URI first. Confirm MIME with `ClipDescription.hasMimeType("image/*")` or `ContentResolver.getType(uri)`. Copy the first readable image immediately through `GeneratedSendFiles.copyImage(generatedSendRoot(), "clipboard-image", extension, input)`. If no image is usable, use the first non-empty `item.coerceToText(this)` and call `GeneratedSendFiles.createText(generatedSendRoot(), "clipboard-text", text)`. Otherwise toast `toast_send_clipboard_unsupported`.

On any generation/copy failure, delete the uncommitted owned file through `GeneratedSendFiles.deleteOwned(...)`, leave the list unchanged, and toast `toast_send_content_create_failed`.

- [ ] **Step 5: Add exact English and Chinese resources and run focused tests**

Add resources including:

```xml
<string name="send_clipboard_unsupported">Clipboard has no text or image to add</string>
<string name="send_content_create_failed">Could not add this content</string>
<string name="send_media_partial_failed">Added available media; %1$d item(s) could not be read</string>
```

```xml
<string name="send_clipboard_unsupported">剪贴板中没有可添加的文字或图片</string>
<string name="send_content_create_failed">无法添加此内容</string>
<string name="send_media_partial_failed">已添加可用媒体，另有 %1$d 项无法读取</string>
```

Run:

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidSendImportContractTest --tests cn.threatexpert.gonc.GeneratedSendFilesTest
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```powershell
git add android/app/src/main/java/cn/threatexpert/gonc/ModuleHost.java android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java android/app/src/main/res/values/strings.xml android/app/src/main/res/values-zh/strings.xml android/app/src/test/java/cn/threatexpert/gonc/AndroidSendImportContractTest.java
git commit -m "feat(android): import all send content types"
```

---

### Task 3: Approved Send List and Add Dialog

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/SendController.java:70-190,400-430`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/UiKit.java:95-225`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/src/main/res/values-zh/strings.xml`
- Create: `android/app/src/main/res/drawable/ic_send_file.xml`
- Create: `android/app/src/main/res/drawable/ic_send_folder.xml`
- Create: `android/app/src/main/res/drawable/ic_send_media.xml`
- Create: `android/app/src/main/res/drawable/ic_send_text.xml`
- Create: `android/app/src/main/res/drawable/ic_send_clipboard.xml`
- Create: `android/app/src/main/res/drawable/ic_send_play.xml`
- Test: `android/app/src/test/java/cn/threatexpert/gonc/AndroidSendListContractTest.java`

**Interfaces:**
- Consumes: Task 1 owned-file cleanup and Task 2 host import methods.
- Produces: `SendController.clearItems()`, `SendController.showAddTypeDialog()`, `SendController.showAuthoredTextDialog()`, and an add-row view passed to Task 5 reveal behavior.

- [ ] **Step 1: Write the failing list UI and mutation contract test**

```java
@Test public void listHasApprovedEmptyPopulatedAndDialogCopy() throws Exception {
    String send = source("SendController.java");
    String zh = resource("values-zh/strings.xml");
    assertFalse(send.contains("host.pickSendFiles());\n        Button addFolder"));
    assertTrue(zh.contains("点击这里添加文件、文件夹、媒体或文字"));
    assertTrue(zh.contains("点击这里继续添加"));
    assertTrue(zh.contains("想加入什么内容？"));
    assertTrue(send.contains("showAddTypeDialog"));
    assertTrue(send.contains("clearItems"));
    assertTrue(send.contains("setText(\"×\")"));
}

@Test public void dialogChoicesAreInConfirmedOrder() throws Exception {
    String send = source("SendController.java");
    int file = send.indexOf("R.string.add_files");
    int folder = send.indexOf("R.string.add_folder", file);
    int media = send.indexOf("R.string.add_media", folder);
    int text = send.indexOf("R.string.add_text", media);
    int clipboard = send.indexOf("R.string.add_clipboard", text);
    assertTrue(file >= 0 && file < folder && folder < media && media < text && text < clipboard);
}
```

Also assert that `removeItem` and `clearItems` both call `syncSource()`, delete only `item.ownedFile()`, and call `host.requestRender()`.

- [ ] **Step 2: Run the list contract test and verify it fails**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidSendListContractTest
```

Expected: FAIL because permanent buttons remain and the dialog/clear/`×` implementation is absent.

- [ ] **Step 3: Consolidate list mutations and ownership cleanup**

Make `addFiles` return whether at least one URI was genuinely new while preserving insertion order. Keep `addFolder` as a one-item delegate. Add:

```java
private void deleteOwned(ShareItem item) {
    if (item.ownedFile() != null) {
        GeneratedSendFiles.deleteOwned(generatedSendRoot(), item.ownedFile());
    }
}

private File generatedSendRoot() {
    return new File(host.context().getCacheDir(), "generated-send");
}

private void clearItems() {
    List<ShareItem> removed = new ArrayList<>(shareItems);
    shareItems.clear();
    syncSource();
    for (ShareItem item : removed) deleteOwned(item);
    host.requestRender();
}
```

Use the same cleanup after a successful single-item removal. Do not delete a replaced duplicate because the existing URI entry remains authoritative. `resetForFreshLaunch()` clears and deletes owned items; `stop()` retains both list and files.

- [ ] **Step 4: Implement the approved empty and populated list**

Build a section header row with `发送内容` or `发送内容 · N` and a quiet clear action only when non-empty. The empty clickable view uses `send_empty_add_hint`. Each row uses 48dp leading space, one-line name, secondary metadata, and a 32dp circular `×` control with content description `移除 <name>`.

Append a full-width `send_continue_add` clickable view after populated rows. Both add surfaces call `showAddTypeDialog()`.

- [ ] **Step 5: Implement the icon-and-label add dialog and authored-text dialog**

Build a custom vertical dialog body with five 48dp-minimum rows. Each row contains a 24dp vector icon and localized label, and calls exactly one host method. The text row opens a multiline `EditText` dialog:

```java
new AlertDialog.Builder(host.context())
    .setTitle(R.string.add_text_title)
    .setView(input)
    .setNegativeButton(R.string.cancel, null)
    .setPositiveButton(R.string.add, null);
```

Override the positive click after `show()` so an empty string leaves the dialog open. A non-empty string calls `host.addAuthoredSendText(value)` and dismisses.

Create 24x24 vector drawables with `viewportWidth/Height="24"`, `fillColor="@android:color/transparent"`, blue stroke color, 1.8-equivalent stroke width, and simple file/folder/media/text/clipboard/play paths.

- [ ] **Step 6: Add localized UI copy and make the focused tests pass**

Required Chinese resources:

```xml
<string name="send_content_title">发送内容</string>
<string name="send_content_title_count">发送内容 · %1$d</string>
<string name="send_empty_add_hint">点击这里添加文件、文件夹、媒体或文字</string>
<string name="send_continue_add">点击这里继续添加</string>
<string name="send_add_type_title">想加入什么内容？</string>
<string name="add_media">媒体</string>
<string name="add_text">文字</string>
<string name="add_clipboard">剪贴板</string>
<string name="add_text_title">添加文字</string>
```

Run:

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidSendListContractTest --tests cn.threatexpert.gonc.AndroidSendImportContractTest
```

Expected: PASS.

- [ ] **Step 7: Commit Task 3**

```powershell
git add android/app/src/main/java/cn/threatexpert/gonc/SendController.java android/app/src/main/java/cn/threatexpert/gonc/UiKit.java android/app/src/main/res/values android/app/src/main/res/values-zh android/app/src/main/res/drawable android/app/src/test/java/cn/threatexpert/gonc/AndroidSendListContractTest.java
git commit -m "feat(android): make send list the add entry"
```

---

### Task 4: Bounded Image and Video Thumbnails

**Files:**
- Create: `android/app/src/main/java/cn/threatexpert/gonc/SendThumbnailLoader.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/SendController.java:120-195`
- Test: `android/app/src/test/java/cn/threatexpert/gonc/SendThumbnailLoaderTest.java`
- Modify test: `android/app/src/test/java/cn/threatexpert/gonc/AndroidSendListContractTest.java`

**Interfaces:**
- Consumes: `ShareItem.uri()`, `ShareItem.mimeType()`, a 48dp target, and a render-generation token.
- Produces: `SendThumbnailLoader.isImage(String)`, `isVideo(String)`, `isCurrent(long,long)`, `load(ShareItem,int,long,Callback)`, and `clear()`.

- [ ] **Step 1: Write failing pure policy and source-contract tests**

```java
@Test public void classifiesOnlyImagesAndVideos() {
    assertTrue(SendThumbnailLoader.isImage("image/png"));
    assertTrue(SendThumbnailLoader.isVideo("video/mp4"));
    assertFalse(SendThumbnailLoader.isImage("text/plain"));
    assertFalse(SendThumbnailLoader.isVideo("audio/mpeg"));
}

@Test public void staleGenerationCannotApply() {
    assertTrue(SendThumbnailLoader.isCurrent(8, 8));
    assertFalse(SendThumbnailLoader.isCurrent(8, 9));
}
```

The source contract asserts `LruCache`, a single bounded executor, `MediaMetadataRetriever`, no unbounded full-image decode, and a play overlay for video rows.

- [ ] **Step 2: Run the focused thumbnail test and verify it fails**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.SendThumbnailLoaderTest
```

Expected: compilation fails because `SendThumbnailLoader` does not exist.

- [ ] **Step 3: Implement bounded asynchronous thumbnail loading**

Use one `ExecutorService` with at most two worker threads and an `LruCache<String, Bitmap>` capped by bitmap byte count. Key by URI, MIME, and target pixel size. On API 29+, load image thumbnails through `ContentResolver.loadThumbnail(uri, new Size(px, px), null)`. On API 26-28, decode image bounds first and choose an `inSampleSize` that does not exceed a small multiple of the 48dp target. For video, use `MediaMetadataRetriever.setDataSource(context, uri)` and `getFrameAtTime(-1, OPTION_CLOSEST_SYNC)`, then center-crop/scale.

Post callbacks through `Handler(Looper.getMainLooper())`. Before applying, require both the requested render generation and row-bound URI to remain current. Catch provider/codec/runtime failures and return `null`, which means keep the type icon.

- [ ] **Step 4: Integrate thumbnails into every image/video row**

Increment `renderGeneration` at the start of every `panel()` build. Start each row with its vector type icon. For image/video MIME types call `thumbnailLoader.load(...)`; replace only the matching row's image when the generation and URI still match. Add a centered play drawable overlay only for video.

The controller and its bounded executor live for the Android process lifetime. `shutdown()` and `resetForFreshLaunch()` increment the render generation and call `thumbnailLoader.clear()` so queued stale results cannot paint and cached bitmaps are released; the loader remains reusable if the retained controller is attached again.

- [ ] **Step 5: Run thumbnail and list tests, then commit**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.SendThumbnailLoaderTest --tests cn.threatexpert.gonc.AndroidSendListContractTest
```

Expected: PASS.

```powershell
git add android/app/src/main/java/cn/threatexpert/gonc/SendThumbnailLoader.java android/app/src/main/java/cn/threatexpert/gonc/SendController.java android/app/src/test/java/cn/threatexpert/gonc/SendThumbnailLoaderTest.java android/app/src/test/java/cn/threatexpert/gonc/AndroidSendListContractTest.java
git commit -m "feat(android): preview send media rows"
```

---

### Task 5: Reveal the Continue-Add Row After Real Additions

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ModuleHost.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java:90-120,345-380,408-425,1565-1590`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/SendController.java:70-160`
- Modify test: `android/app/src/test/java/cn/threatexpert/gonc/AndroidSendListContractTest.java`

**Interfaces:**
- Consumes: Task 3's boolean real-add result and final continue-add view.
- Produces: `ModuleHost.revealAfterRender(View target)` and one-shot `SendController.revealAddAfterNextRender` behavior.

- [ ] **Step 1: Add a failing scroll ownership contract**

```java
@Test public void onlyRealAdditionRequestsRevealAfterRender() throws Exception {
    String send = source("SendController.java");
    String host = source("ModuleHost.java");
    String activity = source("MainActivity.java");
    assertTrue(host.contains("void revealAfterRender(View target);"));
    assertTrue(send.contains("revealAddAfterNextRender"));
    assertTrue(send.contains("if (added)"));
    assertTrue(send.contains("host.revealAfterRender(continueAdd)"));
    assertTrue(activity.contains("mainScrollView.smoothScrollBy"));
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidSendListContractTest
```

Expected: FAIL because the host reveal seam and one-shot flag are absent.

- [ ] **Step 3: Implement one-shot post-render reveal**

Store the root `ScrollView` in `MainActivity.mainScrollView`. Implement `revealAfterRender(View target)` with `target.post(...)`, compare the target's bottom window coordinate with the scroll viewport bottom, and call `smoothScrollBy(0, overlap + dp(12))` only when needed.

In `SendController`, set `revealAddAfterNextRender = true` only when URI merging grows the list. During the next populated `panel()` build, after creating `continueAdd`, clear the flag and call `host.revealAfterRender(continueAdd)`. Duplicate-only selection, cancellation, removal, and clear-all never set the flag.

- [ ] **Step 4: Run the complete Android unit suite and commit**

```powershell
.\gradlew.bat testDebugUnitTest
```

Expected: all Android unit tests PASS.

```powershell
git add android/app/src/main/java/cn/threatexpert/gonc/ModuleHost.java android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java android/app/src/main/java/cn/threatexpert/gonc/SendController.java android/app/src/test/java/cn/threatexpert/gonc/AndroidSendListContractTest.java
git commit -m "feat(android): reveal send add row after additions"
```

---

### Task 6: Full Android Build and Scope Verification

**Files:**
- Modify only files already listed if verification exposes a defect.
- Do not modify: `../gonetcat/**`, `android/app/libs/mobilegonc.aar`.

**Interfaces:**
- Consumes: Tasks 1-5.
- Produces: verified Debug APK and a clean task-owned diff.

- [ ] **Step 1: Run the complete Android test suite without cached test results**

```powershell
.\gradlew.bat cleanTestDebugUnitTest testDebugUnitTest
```

Expected: `BUILD SUCCESSFUL`, zero failed tests.

- [ ] **Step 2: Assemble the Debug APK**

```powershell
.\gradlew.bat assembleDebug
```

Expected: `BUILD SUCCESSFUL` and `android/app/build/outputs/apk/debug/app-debug.apk` exists.

- [ ] **Step 3: Verify scope and generated artifact stability**

```powershell
git diff --check HEAD~5..HEAD
git diff --name-only HEAD~5..HEAD
git status --short
```

Expected: no whitespace errors; changed implementation files remain under `android/` plus this plan/spec history; neither `../gonetcat` nor `android/app/libs/mobilegonc.aar` changed.

- [ ] **Step 4: Perform device or emulator smoke testing when available**

Verify these exact states:

1. Empty sender shows the approved clickable copy and no permanent file/folder buttons.
2. Five picker choices appear in order with icons.
3. Image and video media rows show thumbnails; video shows a play overlay.
4. Authored text and clipboard text/image append with distinct names.
5. Each `×` removes one item; clear-all empties the list while an active sender keeps running.
6. A genuine addition scrolls the continue-add row into view.

If no device/emulator is available, record that device visual smoke was not performed; do not claim it was.

- [ ] **Step 5: Commit any verification-only correction, otherwise leave history unchanged**

If a correction was required:

```powershell
git add android/app/src/main android/app/src/test
git commit -m "fix(android): finalize send content picker"
```

If no correction was required, do not create an empty commit.
