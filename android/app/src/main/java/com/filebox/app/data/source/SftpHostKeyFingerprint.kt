package com.mounvix.app.data.source

import net.schmizz.sshj.transport.verification.FingerprintVerifier
import net.schmizz.sshj.transport.verification.HostKeyVerifier

/** Strict OpenSSH SHA-256 host-key pin parsing and SSHJ verifier creation. */
object SftpHostKeyFingerprint {
    private val pattern = Regex("^SHA256:[A-Za-z0-9+/]{43}=?$")

    fun isValid(value: String): Boolean = pattern.matches(value.trim())

    fun normalize(value: String): String {
        val normalized = value.trim().removeSuffix("=")
        require(pattern.matches(normalized)) {
            "A verified SHA256: SFTP host-key fingerprint is required"
        }
        return normalized
    }

    fun verifier(value: String): HostKeyVerifier =
        FingerprintVerifier.getInstance(normalize(value))
}
