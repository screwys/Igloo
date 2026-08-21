package com.screwy.igloo.net

import io.ktor.http.encodeURLPathPart

fun androidSyncAssetPath(assetId: String, revision: Long): String =
    "/api/android/sync/assets/${assetId.encodeURLPathPart()}/file?revision=$revision"

const val ANDROID_SYNC_ASSET_PACK_PATH = "/api/android/sync/assets/pack"
