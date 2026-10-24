package dev.benyamin.kumiho.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Test

class KumihoShapesTest {

    @Test
    fun `extra large keeps the house dialog radius`() {
        // Material3's AlertDialogDefaults reads shapes.extraLarge. If this slot
        // becomes the full-radius pill, dialog content is clipped at the corners.
        assertEquals(RoundedCornerShape(16.dp), KumihoShapes.extraLarge)
        assertEquals(RoundedCornerShape(percent = 50), PillShape)
    }
}
