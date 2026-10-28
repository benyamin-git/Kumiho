package dev.benyamin.kumiho.tv

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.graphics.BitmapFactory
import android.net.Uri
import android.util.Log
import androidx.tvprovider.media.tv.PreviewChannel
import androidx.tvprovider.media.tv.PreviewChannelHelper
import androidx.tvprovider.media.tv.PreviewProgram
import androidx.tvprovider.media.tv.TvContractCompat
import dev.benyamin.kumiho.R
import java.util.concurrent.TimeUnit

/**
 * Publishes Kumiho's preview channel and its two cards to the Google TV home
 * screen. The channel is app-owned: the launcher shows it under the app's row
 * and opens [MainActivity] with the card's intent. Publishing is best-effort;
 * a launcher that rejects app channels must never crash Kumiho.
 */
object TvChannelPublisher {
    private const val TAG = "KumihoTvChannel"
    private const val PROVIDER_ID = "kumiho-tv-channel"
    private const val OPEN_ID = "kumiho-tv-open"
    private const val CONNECT_ID = "kumiho-tv-connect"

    /** Deep links the channel cards use; MainActivity handles connect. */
    const val CONNECT_URI = "kumiho://tv/connect"
    const val OPEN_URI = "kumiho://tv/open"

    fun publish(context: Context) {
        if (!TvEnvironment.isTelevision(context)) return
        runCatching { publishChannel(context) }
            .onFailure { Log.w(TAG, "Could not publish the TV channel", it) }
    }

    private fun publishChannel(context: Context) {
        val helper = PreviewChannelHelper(context)
        val logo = BitmapFactory.decodeResource(context.resources, R.drawable.tv_channel_logo)

        val channel = PreviewChannel.Builder()
            .setDisplayName(context.getString(R.string.app_name))
            .setDescription(context.getString(R.string.tv_channel_description))
            .setAppLinkIntentUri(Uri.parse(OPEN_URI).toIntentUri())
            .setInternalProviderId(PROVIDER_ID)
            .apply { if (logo != null) setLogo(logo) }
            .build()

        val existing = helper.allChannels.firstOrNull { it.internalProviderId == PROVIDER_ID }
        val channelId = if (existing != null) {
            helper.updatePreviewChannel(existing.id, channel)
            existing.id
        } else {
            helper.publishDefaultChannel(channel)
        }

        deletePrograms(context, helper, channelId)

        val now = System.currentTimeMillis()
        val openId = publishProgram(
            context = context,
            helper = helper,
            channelId = channelId,
            providerId = OPEN_ID,
            title = context.getString(R.string.tv_channel_open_title),
            description = context.getString(R.string.tv_channel_open_text),
            poster = R.drawable.tv_channel_open_v2,
            deepLink = OPEN_URI,
            start = now,
        )
        val connectId = publishProgram(
            context = context,
            helper = helper,
            channelId = channelId,
            providerId = CONNECT_ID,
            title = context.getString(R.string.tv_channel_connect_title),
            description = context.getString(R.string.tv_channel_connect_text),
            poster = R.drawable.tv_channel_connect_v2,
            deepLink = CONNECT_URI,
            start = now + TimeUnit.SECONDS.toMillis(1),
        )
        Log.i(TAG, "Published channel $channelId with programs $openId and $connectId")
    }

    private fun deletePrograms(
        context: Context,
        helper: PreviewChannelHelper,
        channelId: Long,
    ) {
        // TvProvider forbids a selection on preview_program ("Selection not
        // allowed"), so read the small preview-program table and filter by
        // channel client-side.
        val ids = mutableListOf<Long>()
        context.contentResolver.query(
            TvContractCompat.PreviewPrograms.CONTENT_URI,
            arrayOf("_id", TvContractCompat.PreviewPrograms.COLUMN_CHANNEL_ID),
            null,
            null,
            null,
        )?.use { cursor ->
            val idColumn = cursor.getColumnIndexOrThrow("_id")
            val channelColumn = cursor.getColumnIndexOrThrow(
                TvContractCompat.PreviewPrograms.COLUMN_CHANNEL_ID,
            )
            while (cursor.moveToNext()) {
                if (cursor.getLong(channelColumn) == channelId) ids += cursor.getLong(idColumn)
            }
        }
        ids.forEach(helper::deletePreviewProgram)
    }

    @SuppressLint("RestrictedApi")
    private fun publishProgram(
        context: Context,
        helper: PreviewChannelHelper,
        channelId: Long,
        providerId: String,
        title: String,
        description: String,
        poster: Int,
        deepLink: String,
        start: Long,
    ): Long {
        val program = PreviewProgram.Builder()
            .setChannelId(channelId)
            .setType(TvContractCompat.PreviewProgramColumns.TYPE_CLIP)
            .setTitle(title)
            .setDescription(description)
            .setPosterArtUri(resourceUri(context, poster))
            .setIntentUri(Uri.parse(deepLink).toIntentUri())
            .setInternalProviderId(providerId)
            .setStartTimeUtcMillis(start)
            .setEndTimeUtcMillis(start + TimeUnit.HOURS.toMillis(1))
            .build()
        return helper.publishPreviewProgram(program)
    }

    private fun resourceUri(context: Context, res: Int): Uri {
        val type = context.resources.getResourceTypeName(res)
        val name = context.resources.getResourceEntryName(res)
        return Uri.parse("android.resource://${context.packageName}/$type/$name")
    }

    private fun Uri.toIntentUri(): Uri =
        Uri.parse(Intent(Intent.ACTION_VIEW, this).toUri(Intent.URI_INTENT_SCHEME))
}
