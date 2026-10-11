package io.github.misunders2d.agentnet

import org.junit.Assert.*
import org.junit.Test

class NetworkRouteTest {
    @Test fun identityChangesOnlyAndLateLossDoesNotClearNewRoute() {
        val route = NetworkRoute()
        assertTrue(route.available(1))
        assertFalse(route.available(1))
        assertTrue(route.available(2))
        assertFalse(route.lost(1))
        assertFalse(route.available(2))
        assertTrue(route.lost(2))
        assertFalse(route.lost(2))
        assertTrue(route.available(2))
    }
}
