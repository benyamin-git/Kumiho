package dev.benyamin.kumiho.update

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The exact-name checksum lookup used before an APK may reach the installer. */
class UpdateFilesTest {

    @Test
    fun `the named apk wins and the hash is lowercased`() {
        val hash = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"
        val sums = """
            $hash  Kumiho-v0.14.0-Android-arm64-v8a.apk
            ${"0".repeat(64)}  Kumiho-v0.14.0-Android-universal.apk
        """.trimIndent()

        assertEquals(
            hash.lowercase(),
            checksumFor(sums, "Kumiho-v0.14.0-Android-arm64-v8a.apk"),
        )
    }

    @Test
    fun `the binary marker and extra spacing are tolerated`() {
        val hash = "a".repeat(64)
        assertEquals(hash, checksumFor("$hash *Kumiho.apk\n", "Kumiho.apk"))
    }

    @Test
    fun `a missing name or a malformed hash returns null`() {
        assertNull(checksumFor("${"a".repeat(64)}  other.apk\n", "Kumiho.apk"))
        assertNull(checksumFor("not-a-hash  Kumiho.apk\n", "Kumiho.apk"))
    }
}
