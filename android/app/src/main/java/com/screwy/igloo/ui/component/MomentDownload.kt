package com.screwy.igloo.ui.component

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import android.webkit.MimeTypeMap
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.screwy.igloo.R
import com.screwy.igloo.data.dao.AndroidSyncDao
import com.screwy.igloo.data.entity.AndroidSyncAssetEntity
import com.screwy.igloo.media.assetOwnerKind
import com.screwy.igloo.net.ServerBaseUrlProvider
import com.screwy.igloo.net.androidSyncAssetPath
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import io.ktor.client.HttpClient
import io.ktor.client.request.prepareGet
import io.ktor.client.statement.bodyAsChannel
import io.ktor.utils.io.readAvailable
import java.io.File
import java.io.OutputStream
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.koin.compose.koinInject

@Composable
internal fun rememberMomentDownload(): (MomentItem) -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val dao: AndroidSyncDao = koinInject()
    val client: HttpClient = koinInject()
    val server: ServerBaseUrlProvider = koinInject()
    val effects: UiEffects = koinInject()
    var pendingOwnerId by rememberSaveable { mutableStateOf("") }
    var pendingOwnerKind by rememberSaveable { mutableStateOf("") }
    var pendingName by rememberSaveable { mutableStateOf("") }
    val save: (Uri, Boolean) -> Unit = { destination, folder ->
        val ownerId = pendingOwnerId
        val ownerKind = pendingOwnerKind
        val name = pendingName
        pendingOwnerId = ""
        if (ownerId.isNotEmpty()) {
            scope.launch {
                try {
                    withContext(Dispatchers.IO) {
                        val assets = momentDownloadAssets(dao.assetsForOwner(ownerKind, ownerId))
                        check(assets.isNotEmpty())
                        val parent = if (folder) DocumentsContract.buildDocumentUriUsingTree(
                            destination, DocumentsContract.getTreeDocumentId(destination)
                        ) else destination
                        for (asset in assets) {
                            val uri = if (folder) checkNotNull(DocumentsContract.createDocument(
                                context.contentResolver, parent,
                                asset.contentType ?: "application/octet-stream",
                                "${name}_${asset.assetKind}_${asset.mediaIndex + 1}.${momentAssetExtension(asset)}",
                            )) else destination
                            try {
                                checkNotNull(context.contentResolver.openOutputStream(uri)).use {
                                    copyMomentAsset(asset, client, server, it)
                                }
                            } catch (error: Exception) {
                                withContext(kotlinx.coroutines.NonCancellable) {
                                    runCatching { context.contentResolver.delete(uri, null, null) }
                                }
                                throw error
                            }
                        }
                    }
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    effects.emit(UiEffect.ToastRes(R.string.temp_download_status_failed))
                }
            }
        }
    }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val uri = result.data?.data
        if (result.resultCode == Activity.RESULT_OK && uri != null) save(uri, false)
        else pendingOwnerId = ""
    }
    val folderLauncher = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
        if (uri != null) save(uri, true)
        else pendingOwnerId = ""
    }
    return { item ->
        scope.launch {
            val assets = momentDownloadAssets(dao.assetsForOwner(item.ownerKind.assetOwnerKind(), item.mediaOwnerId))
            if (assets.isEmpty()) {
                effects.emit(UiEffect.ToastRes(R.string.temp_download_status_failed))
            } else {
                pendingOwnerId = item.mediaOwnerId
                pendingOwnerKind = item.ownerKind.assetOwnerKind()
                pendingName = item.videoId
                if (assets.size > 1) {
                    folderLauncher.launch(null)
                } else {
                    launcher.launch(Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                        addCategory(Intent.CATEGORY_OPENABLE)
                        type = assets.single().contentType ?: "application/octet-stream"
                        putExtra(Intent.EXTRA_TITLE, "${item.videoId}.${momentAssetExtension(assets.single())}")
                    })
                }
            }
        }
    }
}

private fun momentDownloadAssets(rows: List<AndroidSyncAssetEntity>): List<AndroidSyncAssetEntity> {
    val stream = rows.firstOrNull { it.assetKind == "video_stream" }
        ?: rows.firstOrNull { it.assetKind == "post_media" && it.contentType.orEmpty().startsWith("video/") }
    return if (stream != null) listOf(stream) else rows.filter {
        it.assetKind == "post_media" || it.assetKind == "post_audio"
    }.sortedWith(compareBy({ it.assetKind }, { it.mediaIndex }))
}

private fun momentAssetExtension(asset: AndroidSyncAssetEntity): String =
    MimeTypeMap.getSingleton().getExtensionFromMimeType(asset.contentType) ?: "bin"

private suspend fun copyMomentAsset(
    asset: AndroidSyncAssetEntity,
    client: HttpClient,
    server: ServerBaseUrlProvider,
    output: OutputStream,
) {
    val local = asset.localPath?.let(::File)
    if (local != null && local.isFile) {
        local.inputStream().use { it.copyTo(output) }
        return
    }
    client.prepareGet(server.baseUrl().trimEnd('/') + androidSyncAssetPath(asset.assetId, asset.revision)).execute { response ->
        check(response.status.value in 200..299)
        val channel = response.bodyAsChannel()
        val buffer = ByteArray(64 * 1024)
        while (true) {
            val count = channel.readAvailable(buffer, 0, buffer.size)
            if (count == -1) break
            if (count > 0) output.write(buffer, 0, count)
        }
    }
}
