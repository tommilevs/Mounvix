package com.mounvix.app.data.autoupload

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AutouploadPlannerTest {

    private fun f(name: String, modified: Long) = AutoFile(name, 10, modified, "content://x/$name")

    @Test
    fun onlyNewMediaSortedByTime() {
        val files = listOf(
            f("old.jpg", 100),
            f("new.png", 300),
            f("video.mp4", 200),
            f("notes.txt", 400),
            f("photo.JPEG", 50),
        )
        val plan = AutouploadPlanner.plan(files, lastMillis = 120)
        assertEquals(listOf("video.mp4", "new.png"), plan.map { it.name })
    }

    @Test
    fun zeroLastMillisUploadsAllMedia() {
        val plan = AutouploadPlanner.plan(
            listOf(f("a.jpg", 1), f("b.txt", 2), f("c.mkv", 3)),
            0
        )
        assertEquals(listOf("a.jpg", "c.mkv"), plan.map { it.name })
    }

    @Test
    fun isMediaDetectsImagesAndVideos() {
        assertTrue(AutouploadPlanner.isMedia("IMG_0001.HEIC"))
        assertTrue(AutouploadPlanner.isMedia("clip.webm"))
        assertFalse(AutouploadPlanner.isMedia("document.pdf"))
        assertFalse(AutouploadPlanner.isMedia("noext"))
    }

    @Test
    fun futureFilesIncluded() {
        val plan = AutouploadPlanner.plan(listOf(f("new.jpg", 10_000)), 9_000)
        assertEquals(1, plan.size)
    }
}
