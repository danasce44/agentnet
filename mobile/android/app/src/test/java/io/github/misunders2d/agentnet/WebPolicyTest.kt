package io.github.misunders2d.agentnet

import org.junit.Assert.*
import org.junit.Test

class WebPolicyTest {
    private val policy = WebPolicy("http://127.0.0.1:41327")
    @Test fun scannerTextIsBoundedWithoutOpeningOrReinterpretingIt() {
        val untrusted = "https://example.com/#agentnet-link-v2:untrusted"
        assertEquals(untrusted, WebPolicy.scanText(untrusted))
        assertEquals("x".repeat(16 * 1024), WebPolicy.scanText("x".repeat(16 * 1024)))
        for (text in listOf("", "   ", "x".repeat(16 * 1024 + 1), "€".repeat(6000))) {
            try { WebPolicy.scanText(text); fail("Oversized or empty QR text was accepted") }
            catch (_: IllegalArgumentException) { }
        }
    }
    @Test fun exactAssignedOriginOnly() {
        assertTrue(policy.internal("http://127.0.0.1:41327/api/overview"))
        for (url in listOf("http://127.0.0.1:41328/", "https://127.0.0.1:41327/", "http://localhost:41327/", "http://127.0.0.1.evil:41327/", "http://user@127.0.0.1:41327/", "file:///private/keys", "content://private/keys", "javascript:alert(1)", "blob:http://127.0.0.1:41327/id")) assertFalse(url, policy.internal(url))
    }
    @Test fun bridgeRequiresExactOriginAndMainFrame() {
        assertTrue(policy.bridge("http://127.0.0.1:41327", true))
        assertFalse(policy.bridge("http://127.0.0.1:41327", false))
        assertFalse(policy.bridge("https://example.com", true))
        assertFalse(policy.bridge("http://127.0.0.1:41328", true))
    }
    @Test fun galleryAllowsOnlyFixedRepositoryReadSubresources() {
        val latest = "https://api.github.com/repos/misunders2d/agentnet-skins/releases/latest"
        val raw = "https://raw.githubusercontent.com/misunders2d/agentnet-skins/v2026.10.13/published/pulp/theme.css"
        for (url in listOf(latest, raw, latest.replace("api.github.com", "api.github.com:443"))) {
            assertTrue(url, policy.gallery(url, "GET", false))
            assertFalse(url, policy.gallery(url, "GET", true))
            for (method in listOf("POST", "PUT", "DELETE", "HEAD", "OPTIONS")) assertFalse(method, policy.gallery(url, method, false))
            assertFalse(url, policy.internal(url))
            assertFalse(url, policy.bridge(url, true))
        }
        for (url in listOf(
            latest.replace("https:", "http:"), latest.replace("api.github.com", "user@api.github.com"),
            latest.replace("api.github.com", "api.github.com:8443"), latest + "?private=1", latest + "#fragment",
            latest + "/", latest.replace("agentnet-skins", "other-repository"),
            latest.replace("misunders2d", "other-owner"), latest.replace("api.github.com", "api.github.com.evil"),
            raw.replace("agentnet-skins/", "agentnet-skins-evil/"), raw.replace("published/", "../published/"),
            raw.replace("published/", "./published/"), raw.replace("published/", "%2e%2e/published/"),
            raw.replace("pulp/theme", "pulp%2ftheme"), raw.replace("pulp/theme", "pulp%252ftheme"),
            raw.replace("published/", "published//"), raw + "?private=1", raw + "#fragment", "not a URL"
        )) assertFalse(url, policy.gallery(url, "GET", false))
    }
    @Test fun notificationDestinationsKeepExactWorkspaceAndDirection() {
        val workspace = "a".repeat(32); val conv = "b".repeat(64); val msg = "c".repeat(32)
        assertEquals("msg=$msg&conv=$conv&dir=out&workspace=$workspace", WebPolicy.notificationDestination(workspace, "msg=$msg&conv=$conv&dir=out"))
        assertEquals("workspace=default", WebPolicy.notificationDestination("default", ""))
        assertEquals("workspace=$workspace", WebPolicy.notificationDestination(workspace, ""))
        assertEquals("workspace=$workspace", WebPolicy.notificationDestination(workspace, "workspace=$workspace"))
        assertEquals("workspace=default", WebPolicy.notificationDestination("default", "workspace=default"))
        assertEquals("review&workspace=default", WebPolicy.notificationDestination("default", "review"))
        assertEquals("conv=$conv&workspace=$workspace", WebPolicy.notificationDestination(workspace, "conv=$conv"))
        assertNotEquals(WebPolicy.notificationDestination("default", "conv=$conv"), WebPolicy.notificationDestination(workspace, "conv=$conv"))
        for (fragment in listOf("conv=$conv&workspace=default", "conv=$conv&conv=$conv", "msg=$msg&dir=unknown", "review&conv=$conv", "conv=$conv&extra=1", "conv=broken")) assertNull(WebPolicy.notificationDestination(workspace, fragment))
        assertNull(WebPolicy.notificationDestination("unknown", "conv=$conv"))
    }
    @Test fun externalNavigationHasNoNativeSchemesOrCredentials() {
        assertTrue(policy.external("https://example.com/docs"))
        for (url in listOf("intent://x", "file:///x", "javascript:alert(1)", "https://user@example.com", "https://localhost/x", "https://127.0.0.1/x")) assertFalse(policy.external(url))
    }
}
