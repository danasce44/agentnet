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
import java.util.concurrent.ExecutorService
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
            check(mode in setOf("install", "snapshot", "network", "cleanup", "interrupt", "notifications", "notifications-denied")) { "Unknown fixture operation." }
            if (mode == "cleanup") {
                val preferences=targetContext.applicationContext.getSharedPreferences("native-update",0)
                val id=preferences.getInt("session",-1)
                val installer=targetContext.packageManager.packageInstaller
                val info=installer.getSessionInfo(id)
                check(info?.appPackageName == targetContext.packageName && info.installerPackageName == targetContext.packageName) { "No test-owned fixture session exists." }
                installer.abandonSession(id)
                preferences.edit().remove("session").remove("result").commit()
                result.putString("stream","Discarded test-owned session blocked by harness teardown; this is setup cleanup, not cancellation evidence.\n")
                finish(Activity.RESULT_OK,result); return
            }
            val activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            waitForIdleSync()
            if (mode.startsWith("notifications")) {
                checkNotifications(activity, mode == "notifications-denied", result)
                finish(Activity.RESULT_OK, result); return
            }
            if (mode == "snapshot" || mode == "network") {
                if (mode == "network") {
                    snapshot(activity) // Wait for the real embedded host before changing routes.
                    val host = (targetContext.applicationContext as AgentNetApp).host
                    val monitor = NativeHost::class.java.getDeclaredField("networkMonitor").apply { isAccessible = true }.get(host)
                    val route = monitor.javaClass.getDeclaredField("route").apply { isAccessible = true }.get(monitor)
                    val current = route.javaClass.getDeclaredField("current").apply { isAccessible = true }
                    fun handle(): Long? { var value: Long? = null; runOnMainSync { value = current.get(route) as Long? }; return value }
                    val before = handle()
                    sendStatus(0, Bundle().apply { putString("stream", "Waiting for owned-emulator network change; handle=$before\n") })
                    val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(30)
                    while (handle() == before && System.nanoTime() < deadline) Thread.sleep(100)
                    val after = handle()
                    check(after != before) { "Android did not report a default-route identity change." }
                    val lane = NativeHost::class.java.getDeclaredField("lane").apply { isAccessible = true }.get(host) as ExecutorService
                    val drained = CountDownLatch(1)
                    lane.execute { drained.countDown() }
                    check(drained.await(5, TimeUnit.SECONDS)) { "The native connection lane did not return." }
                    sendStatus(0, Bundle().apply { putString("stream", "Native network route $before -> $after; connection lane returned.\n") })
                }
                val snapshot = snapshot(activity)
                // Instrumentation runs as the target UID, including release targets.
                // Export only this bounded synthetic fixture, never an entry token/key.
                check(snapshot.toByteArray().size <= 256 * 1024) { "The synthetic snapshot exceeds its result bound." }
                val value = JSONObject(snapshot)
                val digest = MessageDigest.getInstance("SHA-256").digest(value.getJSONObject("preserved").toString().toByteArray()).joinToString("") { "%02x".format(it) }
                result.putString("stream", "SNAPSHOT_JSON=$snapshot\nSnapshot version=${value.getString("version")}; messages=${value.getJSONObject("preserved").getJSONArray("messages").length()}; preserved_sha256=$digest\n")
            } else {
                val fixture = File(targetContext.cacheDir, "updater-fixture.apk")
                check(fixture.isFile) { "Stage the signed fixture APK in the owned target's private cache first." }
                val complete = CountDownLatch(1)
                val terminal = CountDownLatch(1)
                val preferences=targetContext.applicationContext.getSharedPreferences("native-update",0)
                val listener=android.content.SharedPreferences.OnSharedPreferenceChangeListener { p,key ->
                    if(key == "result" && p.getInt("session",-1) < 0) terminal.countDown()
                }
                preferences.registerOnSharedPreferenceChangeListener(listener)
                val diagnostic=Bundle().apply { putString("stream", "Before retry: session=${preferences.getInt("session",-1)}, pending=${preferences.getBoolean("pending_confirmation",false)}, target=${targetContext.dataDir}, activity=${activity.dataDir}\n") }; sendStatus(0,diagnostic)
                var response: JSONObject? = null
                var failure: String? = null
                val callback: (JSONObject?, String?) -> Unit = { value, error -> response = value; failure = error; complete.countDown() }
                val method = MainActivity::class.java.getDeclaredMethod("installPreparedUpdate", NativeUpdater.Prepared::class.java, kotlin.jvm.functions.Function2::class.java)
                method.isAccessible = true
                runOnMainSync { method.invoke(activity, NativeUpdater.Prepared(fixture, "v0.8.19"), callback) }
                check(complete.await(120, TimeUnit.SECONDS)) { "Permission/installer continuation did not complete." }
                check(failure == null) { failure.orEmpty() }
                check(response?.optString("state") == "installer") { "The real installer was not started." }
                if(mode == "interrupt") {
                    // Intentional process interruption only after the platform reported
                    // real pending user action; no preference fabrication.
                    val deadline=System.nanoTime()+TimeUnit.SECONDS.toNanos(10)
                    while(!preferences.getBoolean("pending_confirmation",false) && System.nanoTime()<deadline) Thread.sleep(50)
                    check(preferences.getBoolean("pending_confirmation",false)) { "Android did not report pending confirmation." }
                    val id=preferences.getInt("session",-1)
                    val info=targetContext.packageManager.packageInstaller.getSessionInfo(id)
                    check(info != null && !info.isActive) { "The fixture is not waiting for confirmation." }
                    result.putString("stream","Interrupted real inactive confirmation session=$id; explicit visible retry must replace only this owned session.\n")
                    finish(Activity.RESULT_OK,result); return
                }
                // Finishing instrumentation kills the target process. Keep it visible
                // until Android's real callback, or normal package replacement.
                check(terminal.await(120, TimeUnit.SECONDS)) { "Android installation confirmation did not reach a terminal result." }
                preferences.unregisterOnSharedPreferenceChangeListener(listener)
                result.putString("stream", "Real installer callback: ${preferences.getString("result", "")}\n")
            }
            finish(Activity.RESULT_OK, result)
        } catch (e: Exception) {
            result.putString("stream", "Updater fixture failed: ${e.message}\n")
            finish(Activity.RESULT_CANCELED, result)
        }
    }

    /** OS-boundary test on the owned emulator; never grants or changes permissions. */
    private fun checkNotifications(activity: MainActivity, denied: Boolean, result: Bundle) {
        val host = (targetContext.applicationContext as AgentNetApp).host
        // Disposable preferences exercise the actual consent implementation,
        // without changing an Android permission or starting a live service.
        val consentPreferences = targetContext.getSharedPreferences("notification-consent-fixture", 0)
        consentPreferences.edit().clear().commit()
        var starts = 0
        startConnectionAfterConsent(consentPreferences, false) { starts++ }
        check(!consentPreferences.getBoolean("keep-connected", false) && starts == 0 && !consentPreferences.contains("keep-connected"))
        startConnectionAfterConsent(consentPreferences, true) { starts++ }
        check(consentPreferences.getBoolean("keep-connected", false) && starts == 1)
        startConnectionAfterConsent(targetContext.getSharedPreferences("notification-consent-fixture", 0), true) { starts++ }
        check(starts == 1) // saved on survives restart without a second consent start
        consentPreferences.edit().putBoolean("keep-connected", false).commit()
        startConnectionAfterConsent(consentPreferences, true) { starts++ }
        check(!consentPreferences.getBoolean("keep-connected", false) && starts == 1) // explicit off wins
        consentPreferences.edit().clear().commit()
        var refused = false
        try { startConnectionAfterConsent(consentPreferences, true) { throw IllegalStateException("Fixture service refusal") } }
        catch (_: IllegalStateException) { refused = true }
        check(refused && !consentPreferences.getBoolean("keep-connected", false))
        consentPreferences.edit().clear().commit()
        // In-memory OS channel objects exercise the actual status/intent code;
        // no saved app channel, permission, sound mode or DND choice is changed.
        val fresh = activityNotificationChannel()
        val freshStatus = activityNotificationStatus(true, fresh)
        check(freshStatus.getBoolean("granted") && freshStatus.getBoolean("app_granted"))
        check(freshStatus.getJSONObject("channel").getBoolean("sound") && freshStatus.getJSONObject("channel").getBoolean("vibration"))
        fresh.enableVibration(false)
        check(!activityNotificationStatus(true, fresh).getJSONObject("channel").getBoolean("vibration"))
        check(!fresh.shouldVibrate()) // reading a legacy vibration-off channel never changes it
        val muted = android.app.NotificationChannel("fixture-muted", "Fixture", android.app.NotificationManager.IMPORTANCE_NONE)
        check(!activityNotificationStatus(true, muted).getBoolean("granted"))
        check(activityNotificationStatus(true, muted).getBoolean("app_granted"))
        check(!activityNotificationStatus(false, fresh).getBoolean("granted"))
        check(!activityNotificationStatus(true, null).has("channel"))
        val settings = activityNotificationSettings(targetContext.packageName)
        check(settings.action == android.provider.Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS)
        check(settings.getStringExtra(android.provider.Settings.EXTRA_APP_PACKAGE) == targetContext.packageName)
        check(settings.getStringExtra(android.provider.Settings.EXTRA_CHANNEL_ID) == ACTIVITY_CHANNEL)
        val manager = targetContext.getSystemService(android.app.NotificationManager::class.java)
        val post = NativeHost::class.java.getDeclaredMethod("notifyActivity", String::class.java, String::class.java, String::class.java, String::class.java).apply { isAccessible = true }
        val dismiss = NativeHost::class.java.getDeclaredMethod("dismissActivity", String::class.java, String::class.java).apply { isAccessible = true }
        val workspace = "default"
        val routes = listOf("conv=" + "a".repeat(64), "review", "msg=" + "b".repeat(32) + "&dir=in")
        val tags = routes.map { WebPolicy.notificationDestination(workspace, it)!! }.toSet()
        fun active() = manager.activeNotifications.filter { it.tag in tags }
        fun emit(fragment: String) = runOnMainSync { post.invoke(host, workspace, fragment, "Fixture sender", "Locally decrypted fixture preview") }
        fun waitFor(label: String, predicate: () -> Boolean) {
            val end = System.nanoTime() + TimeUnit.SECONDS.toNanos(5)
            while (!predicate() && System.nanoTime() < end) Thread.sleep(50)
            check(predicate()) { label }
        }
        try {
            check(host.notificationsPermitted() != denied) { "Expected Android permission ${if (denied) "denied" else "granted"}; this test never changes it." }
            routes.forEach { route -> runOnMainSync { dismiss.invoke(host, workspace, route) } }
            if (denied) {
                routes.forEach(::emit)
                waitFor("Denied notifications appeared") { active().isEmpty() }
                result.putString("stream", "PASS Android denied permission: no notification posted; permissions unchanged.\n")
                return
            }
            // Foreground, then background, then screen-off arrival use the same
            // actual NotificationManager surface. Core event producers are tested in Go.
            emit(routes[0]); emit(routes[0])
            waitFor("Foreground notification/dedup failed") { active().size == 1 }
            runOnMainSync { activity.moveTaskToBack(true) }
            emit(routes[1])
            waitFor("Background OK notification missing") { active().size == 2 }
            uiAutomation.executeShellCommand("input keyevent 223").close()
            emit(routes[2])
            waitFor("Screen-off message/reminder route missing") { active().size == 3 }
            check(active().all { it.notification.extras.getCharSequence(android.app.Notification.EXTRA_TITLE).toString() == "Fixture sender" })
            check(active().all { it.notification.extras.getCharSequence(android.app.Notification.EXTRA_TEXT).toString() == "Locally decrypted fixture preview" })
            check(active().all { it.notification.publicVersion.extras.getCharSequence(android.app.Notification.EXTRA_TITLE).toString() == "AgentNet" })
            check(active().all { !it.notification.publicVersion.extras.getCharSequence(android.app.Notification.EXTRA_TEXT).toString().contains("fixture preview") })
            check(active().all { it.notification.visibility == android.app.Notification.VISIBILITY_PRIVATE })
            uiAutomation.executeShellCommand("input keyevent 224").close()
            val target = active().single { it.tag == WebPolicy.notificationDestination(workspace, routes[2]) }
            target.notification.contentIntent.send()
            waitFor("Notification tap lost exact destination") {
                var matches = false
                runOnMainSync { matches = activity.intent.getStringExtra("workspace") == workspace && activity.intent.getStringExtra("destination") == routes[2] }
                matches
            }
            runOnMainSync { dismiss.invoke(host, workspace, routes[0]) }
            waitFor("Reading exact chat did not dismiss its notification") { active().none { it.tag == WebPolicy.notificationDestination(workspace, routes[0]) } }
            check(active().any { it.tag == WebPolicy.notificationDestination(workspace, routes[1]) }) { "Reading another chat cleared an OK" }
            result.putString("stream", "PASS Android OS notification boundary: foreground/dedup, background OK, screen-off message/reminder route, private content, exact tap and read dismissal. No physical-phone or relay delivery claim.\n")
        } finally {
            uiAutomation.executeShellCommand("input keyevent 224").close()
            routes.forEach { route -> runOnMainSync { dismiss.invoke(host, workspace, route) } }
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
