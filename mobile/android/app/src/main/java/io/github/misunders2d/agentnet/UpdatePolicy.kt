package io.github.misunders2d.agentnet

import java.net.URI

/** Release metadata is transport input, never a caller-selected install source. */
object UpdatePolicy {
    const val repository = "https://github.com/misunders2d/agentnet"
    const val asset = "AgentNet-android.apk"
    const val certificate = "e1d06246fc1e49d5cc24dddb4dea618c2410cc35e3a98efa28daa8ae450b0659"
    const val maxApk = 160L * 1024 * 1024
    fun attemptFile(directory: java.io.File): java.io.File = java.io.File.createTempFile("agentnet-update-", ".apk", directory)
    fun code(version: String): Int {
        val match = Regex("^v(0|[1-9][0-9]{0,2})\\.(0|[1-9][0-9]{0,2})\\.(0|[1-9][0-9]{0,2})$").matchEntire(version)
            ?: throw IllegalArgumentException("The release version is invalid.")
        val (major, minor, patch) = match.destructured
        return major.toInt() * 1_000_000 + minor.toInt() * 1_000 + patch.toInt()
    }
    fun latest(url: String): String {
        val prefix = "$repository/releases/tag/"
        require(url.startsWith(prefix)) { "The official release address is invalid." }
        val version = url.removePrefix(prefix); code(version); return version
    }
    fun checksum(text: String): String {
        val matches = text.lineSequence().mapNotNull {
            Regex("^([a-fA-F0-9]{64}) [ *]${Regex.escape(asset)}$").matchEntire(it)?.groupValues?.get(1)
        }.toList()
        require(matches.size == 1) { "The release checksum is missing or ambiguous." }
        return matches.single().lowercase()
    }
    fun validateApk(packageName: String, installedPackage: String, signers: List<String>, installedSigners: List<String>,
                    versionCode: Long, installedCode: Long, versionName: String?, expected: String) {
        require(installedPackage == "io.github.misunders2d.agentnet.preview" && packageName == installedPackage) { "The release APK belongs to a different app." }
        require(signers == listOf(certificate) && installedSigners == listOf(certificate)) { "The release APK does not use this preview's original signing key." }
        require(versionCode > installedCode && versionCode == code(expected).toLong() && versionName == expected) { "The release APK version is not the expected newer release." }
    }
    fun downloadURL(version: String, file: String): String {
        code(version); require(file == asset || file == "SHA256SUMS")
        return "$repository/releases/download/$version/$file"
    }
    fun redirect(url: String): Boolean = runCatching {
        val u = URI(url)
        u.scheme == "https" && u.userInfo == null && u.fragment == null && (u.port == -1 || u.port == 443) &&
            when (u.host) {
                "github.com" -> u.rawPath.startsWith("/misunders2d/agentnet/releases/")
                "release-assets.githubusercontent.com", "objects.githubusercontent.com" -> true
                else -> false
            }
    }.getOrDefault(false)
}
