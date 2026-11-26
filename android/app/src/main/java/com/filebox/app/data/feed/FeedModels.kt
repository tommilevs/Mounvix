package com.mounvix.app.data.feed

/** Credentials and base URL for the single active Mounvix memories profile. */
data class FeedProfile(
    val baseUrl: String,
    val username: String,
    val password: String,
)

/** A page returned by the server; committing [nextSince] is an explicit repository operation. */
data class FeedPage(
    val items: List<FeedCard>,
    val nextSince: String,
)

/** One generated memories card. */
data class FeedCard(
    val id: String,
    val generatedAt: String,
    val title: String,
    val media: List<FeedMedia>,
    val isRead: Boolean,
)

/** A root-relative media reference served through the authenticated Mounvix API. */
data class FeedMedia(
    val root: String,
    val path: String,
    val kind: String,
    val modifiedAt: String,
    val thumbnailUrl: String?,
    val fileUrl: String,
)
