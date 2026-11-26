package com.mounvix.app.data.source

import net.schmizz.sshj.common.Buffer
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.util.Base64

class SftpHostKeyFingerprintTest {
    @Test
    fun sha256FingerprintAcceptsOnlyTheMatchingSshHostKey() {
        val trustedKey = generateRsaPublicKey()
        val untrustedKey = generateRsaPublicKey()
        val pin = fingerprintOf(trustedKey)

        val verifier = SftpHostKeyFingerprint.verifier(pin)

        assertTrue(verifier.verify("nas.example", 22, trustedKey))
        assertFalse(verifier.verify("nas.example", 22, untrustedKey))
    }

    @Test
    fun verifierRejectsBlankOrMalformedPinBeforeAnyConnectionAttempt() {
        assertThrows(IllegalArgumentException::class.java) {
            SftpHostKeyFingerprint.verifier("")
        }
        assertThrows(IllegalArgumentException::class.java) {
            SftpHostKeyFingerprint.verifier("MD5:00:11:22")
        }
    }

    @Test
    fun fingerprintParserRequiresCanonicalSha256Fingerprint() {
        val valid = "SHA256:" + "A".repeat(43)

        assertTrue(SftpHostKeyFingerprint.isValid(valid))
        assertTrue(SftpHostKeyFingerprint.isValid("$valid="))
        assertFalse(SftpHostKeyFingerprint.isValid(""))
        assertFalse(SftpHostKeyFingerprint.isValid("MD5:00:11:22"))
        assertFalse(SftpHostKeyFingerprint.isValid("SHA256:short"))
    }

    private fun generateRsaPublicKey() = KeyPairGenerator.getInstance("RSA").apply {
        initialize(2048)
    }.generateKeyPair().public

    private fun fingerprintOf(publicKey: java.security.PublicKey): String {
        val sshBlob = Buffer.PlainBuffer().putPublicKey(publicKey).compactData
        val digest = MessageDigest.getInstance("SHA-256").digest(sshBlob)
        return "SHA256:" + Base64.getEncoder().withoutPadding().encodeToString(digest)
    }
}
