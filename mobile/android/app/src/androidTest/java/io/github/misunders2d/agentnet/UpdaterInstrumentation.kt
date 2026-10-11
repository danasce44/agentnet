package io.github.misunders2d.agentnet

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import android.webkit.WebView
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Test APK only: the owned emulator fixture, never a production bridge or source override. */
class UpdaterInstrumentation : Instrumentation() {
    private var mode = "install"
    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        mode = arguments?.getString("mode") ?: "install"
        start()
    }
    override fun onStart() {
        val result = Bundle()
        try {
            check(mode == "install" || mode == "snapshot") { "Unknown fixture operation." }
            val activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            waitForIdleSync()
            if (mode == "snapshot") {
                val snapshot = snapshot(activity)
                // Test package remains debuggable even when the target update is not.
                // The authenticated entry URL/token and private key are never exported.
                File(context.cacheDir, "updater-snapshot.json").writeText(snapshot)
                val value = JSONObject(snapshot)
                val digest = MessageDigest.getInstance("SHA-256").digest(value.getJSONObject("preserved").toString().toByteArray()).joinToString("") { "%02x".format(it) }
                result.putString("stream", "Snapshot stored in test private cache; version=${value.getString("version")}; messages=${value.getJSONObject("preserved").getJSONArray("messages").length()}; preserved_sha256=$digest\n")
            } else {
                val fixture = File(targetContext.cacheDir, "updater-fixture.apk")
                check(fixture.isFile) { "Stage the signed fixture APK in the owned target's private cache first." }
                val complete = CountDownLatch(1)
                var response: JSONObject? = null
                var failure: String? = null
                val callback: (JSONObject?, String?) -> Unit = { value, error -> response = value; failure = error; complete.countDown() }
                val method = MainActivity::class.java.getDeclaredMethod("installPreparedUpdate", NativeUpdater.Prepared::class.java, kotlin.jvm.functions.Function2::class.java)
                method.isAccessible = true
                runOnMainSync { method.invoke(activity, NativeUpdater.Prepared(fixture, "v0.8.19"), callback) }
                check(complete.await(120, TimeUnit.SECONDS)) { "Permission/installer continuation did not complete." }
                check(failure == null) { failure.orEmpty() }
                check(response?.optString("state") == "installer") { "The real installer was not started." }
                result.putString("stream", "Real PackageInstaller started; observe confirmation/cancellation and native result on the owned emulator.\n")
            }
            finish(Activity.RESULT_OK, result)
        } catch (e: Exception) {
            result.putString("stream", "Updater fixture failed: ${e.message}\n")
            finish(Activity.RESULT_CANCELED, result)
        }
    }
    private fun javascript(page: WebView, script: String): String {
        val complete = CountDownLatch(1)
        var value = "null"
        runOnMainSync { page.evaluateJavascript(script) { value = it; complete.countDown() } }
        check(complete.await(5, TimeUnit.SECONDS)) { "The owned WebView did not respond." }
        return value
    }
    private fun snapshot(activity: MainActivity): String {
        val field = MainActivity::class.java.getDeclaredField("web").apply { isAccessible = true }
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(30)
        var page: WebView? = null
        while (page == null && System.nanoTime() < deadline) {
            runOnMainSync { page = field.get(activity) as? WebView }
            if (page == null) Thread.sleep(100)
        }
        val view = checkNotNull(page) { "The native host did not open." }
        var ready = false
        while (!ready && System.nanoTime() < deadline) {
            ready = javascript(view, "Boolean(window.__agentnetAndroid && document.readyState === 'complete')") == "true"
            if (!ready) Thread.sleep(100)
        }
        check(ready) { "The authenticated local page did not become ready." }
        javascript(view, """
            (function() {
              window.__agentnetUpdaterSnapshot = null;
              Promise.all([
                fetch('/api/overview').then(r => { if (!r.ok) throw Error('overview unavailable'); return r.json(); }),
                fetch('/api/thread?id=00000000000000000000000000000001').then(r => { if (!r.ok) throw Error('synthetic thread unavailable'); return r.json(); }),
                window.__agentnetNativeAppJSON('status', {})
              ]).then(([overview, thread, status]) => {
                const drafts = Object.fromEntries(Object.entries(localStorage).filter(([key]) => /draft/.test(key)).sort(([a],[b]) => a.localeCompare(b)));
                window.__agentnetUpdaterSnapshot = {
                  version: overview.version, installedVersion: status.version, updateResult: status.update_result || '',
                  preserved: {origin: location.origin, identity: overview.me,
                    messages: thread.messages.map(m => ({id:m.id, body:m.body, state:m.state ?? null, files:m.files ?? null})), drafts}
                };
              }).catch(error => { window.__agentnetUpdaterSnapshot = {error: String(error)}; });
              return true;
            })()
        """.trimIndent())
        while (System.nanoTime() < deadline) {
            val encoded = javascript(view, "JSON.stringify(window.__agentnetUpdaterSnapshot)")
            if (encoded != "null" && encoded != "\"null\"") {
                val decoded = JSONObject("{\"value\":$encoded}").getString("value")
                val value = JSONObject(decoded)
                check(!value.has("error")) { value.optString("error") }
                check(value.getJSONObject("preserved").getJSONArray("messages").length() == 203) { "The owned 203-message fixture changed." }
                return decoded
            }
            Thread.sleep(100)
        }
        error("The local fixture snapshot did not complete.")
    }
}
