package dev.benyamin.kumiho.tv

import android.content.res.Configuration
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TvEnvironmentTest {
    private val phone = Configuration.UI_MODE_TYPE_NORMAL
    private val television = Configuration.UI_MODE_TYPE_TELEVISION

    @Test
    fun aPhoneWithoutLeanbackIsNotTelevision() {
        assertFalse(TvEnvironment.isTelevision(uiMode = phone, hasLeanback = false))
    }

    @Test
    fun televisionUiModeIsTelevisionEvenWithoutTheFeature() {
        assertTrue(TvEnvironment.isTelevision(uiMode = television, hasLeanback = false))
    }

    @Test
    fun theLeanbackFeatureIsTelevisionEvenInNormalUiMode() {
        assertTrue(TvEnvironment.isTelevision(uiMode = phone, hasLeanback = true))
    }

    @Test
    fun aTelevisionWithLeanbackIsTelevision() {
        assertTrue(TvEnvironment.isTelevision(uiMode = television, hasLeanback = true))
    }

    @Test
    fun unrelatedUiModeBitsDoNotHideTelevision() {
        val uiMode = television or Configuration.UI_MODE_NIGHT_YES
        assertTrue(TvEnvironment.isTelevision(uiMode = uiMode, hasLeanback = false))
    }
}
