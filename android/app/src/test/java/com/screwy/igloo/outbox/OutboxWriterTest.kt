package com.screwy.igloo.outbox

import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.RoomTestSupport
import com.screwy.igloo.data.entity.ChannelFollowEntity
import com.screwy.igloo.data.entity.ChannelSettingEntity
import com.screwy.igloo.data.entity.FeedLikeEntity
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.MutedChannelEntity
import com.screwy.igloo.data.entity.OutboxEntity
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.cancel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestCoroutineScheduler
import kotlinx.coroutines.test.TestScope
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
@OptIn(ExperimentalCoroutinesApi::class)
class OutboxWriterTest {
    private lateinit var db: IglooDatabase
    private lateinit var scope: CoroutineScope
    private lateinit var prefs: PreferencesRepo
    private lateinit var writer: OutboxWriter
    private lateinit var drainRequests: AtomicInteger

    @Before
    fun setUp() {
        db = RoomTestSupport.freshDb()
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        prefs = PreferencesRepo(db.preferenceDao(), scope, nowMsProvider = { 1_000L })
        drainRequests = AtomicInteger()
        writer =
            OutboxWriter(
                db = db,
                prefs = prefs,
                scope = scope,
                onDrainRequested = { drainRequests.incrementAndGet() },
                nowMsProvider = { 1_000L },
                writeDebounceMs = 1L,
            )
    }

    @After
    fun tearDown() {
        RoomTestSupport.closeAfterScope(scope, db)
    }

    @Test
    fun setIsOptimisticAndCoalescesToLatestAction() = runBlocking {
        writer.enqueue(OutboxKind.Like("item-1", OutboxKind.Action.Set))
        writer.enqueue(OutboxKind.Like("item-1", OutboxKind.Action.Clear))
        writer.enqueue(OutboxKind.Like("item-1", OutboxKind.Action.Set))

        assertTrue(db.feedLikeDao().exists("item-1"))
        assertTrue(db.feedSeenDao().exists("item-1"))
        assertEquals(1, db.outboxDao().countByState("pending"))
        val payload =
            Json.parseToJsonElement(db.outboxDao().pendingRows().single().payloadJson).jsonObject
        assertEquals("set", payload.getValue("action").jsonPrimitive.content)
    }

    @Test
    fun clearDoesNotDeleteCanonicalStateBeforeAck() = runBlocking {
        db.feedLikeDao().upsert(FeedLikeEntity("item-1", 1L))

        writer.enqueue(OutboxKind.Like("item-1", OutboxKind.Action.Clear))

        assertTrue(db.feedLikeDao().exists("item-1"))
    }

    @Test
    fun seenAndMomentViewsCoalesceByItem() = runBlocking {
        writer.enqueue(OutboxKind.Seen("item-1"))
        writer.enqueue(OutboxKind.Seen("item-1"))
        writer.enqueue(OutboxKind.MomentView("video-1"))
        writer.enqueue(OutboxKind.MomentView("video-1"))

        assertEquals(2, db.outboxDao().countByState("pending"))
        assertTrue(db.feedSeenDao().exists("item-1"))
        assertTrue(db.momentViewDao().exists("video-1"))
    }

    @Test
    fun channelMuteUsesServerChannelId() = runBlocking {
        writer.enqueue(OutboxKind.Mute("channel-1", OutboxKind.Action.Set))

        assertTrue(db.mutedChannelDao().exists("channel-1"))
        val payload =
            Json.parseToJsonElement(db.outboxDao().pendingRows().single().payloadJson).jsonObject
        assertEquals("channel-1", payload.getValue("channel_id").jsonPrimitive.content)
    }

    @Test
    fun clearMuteRemovesOptimisticLocalMuteImmediately() = runBlocking {
        db.mutedChannelDao().upsert(MutedChannelEntity("channel-1", 1L))

        writer.enqueue(OutboxKind.Mute("channel-1", OutboxKind.Action.Clear))

        assertFalse(db.mutedChannelDao().exists("channel-1"))
        val payload =
            Json.parseToJsonElement(db.outboxDao().pendingRows().single().payloadJson).jsonObject
        assertEquals("clear", payload.getValue("action").jsonPrimitive.content)
    }

    @Test
    fun includeRepostsIsOptimisticForAnyPlatformChannelId() = runBlocking {
        writer.enqueue(OutboxKind.ChannelSetting("tiktok_channel", "include_reposts", 0))
        writer.enqueue(OutboxKind.ChannelSetting("instagram_channel", "include_reposts", 0))

        assertEquals(0, db.channelSettingDao().getById("tiktok_channel")?.includeReposts)
        assertEquals(0, db.channelSettingDao().getById("instagram_channel")?.includeReposts)
    }

    @Test
    fun selectionWideningIsPersistedWithTheOptimisticOutboxIntent() = runBlocking {
        writer.enqueue(OutboxKind.Follow("new_channel", OutboxKind.Action.Set))

        assertTrue(db.outboxDao().pendingRows().single().selectionWidening())

        db.channelSettingDao()
            .upsert(ChannelSettingEntity("existing_channel", includeReposts = 0, updatedAt = 1L))

        writer.enqueue(
            OutboxKind.ChannelSetting("existing_channel", "include_reposts", 1)
        )

        assertTrue(
            db.outboxDao().pendingRows()
                .single { it.kind == OutboxKind.CODE_CHANNEL_SETTING }
                .selectionWidening()
        )
    }

    @Test
    fun unchangedAndNarrowingSelectionMutationsDoNotRequestReplay() = runBlocking {
        db.channelFollowDao().upsert(ChannelFollowEntity("existing_channel", 1L))
        db.channelSettingDao()
            .upsert(ChannelSettingEntity("existing_channel", includeReposts = 1, updatedAt = 1L))

        writer.enqueue(OutboxKind.Follow("existing_channel", OutboxKind.Action.Set))
        writer.enqueue(
            OutboxKind.ChannelSetting("existing_channel", "include_reposts", 1)
        )
        writer.enqueue(
            OutboxKind.ChannelSetting("existing_channel", "include_reposts", 0)
        )

        assertTrue(db.outboxDao().pendingRows().none(OutboxEntity::selectionWidening))
    }

    @Test
    fun coalescedSelectionMarkerFollowsTheFinalIntentAgainstTheOriginalBaseline() = runBlocking {
        writer.enqueue(OutboxKind.Follow("new_channel", OutboxKind.Action.Set))
        assertTrue(db.outboxDao().pendingRows().single().selectionWidening())

        writer.enqueue(OutboxKind.Follow("new_channel", OutboxKind.Action.Clear))
        assertFalse(db.outboxDao().pendingRows().single().selectionWidening())

        writer.enqueue(OutboxKind.Follow("new_channel", OutboxKind.Action.Set))
        assertTrue(db.outboxDao().pendingRows().single().selectionWidening())

        writer.enqueue(OutboxKind.Follow("new_channel", OutboxKind.Action.Clear))
        assertFalse(db.outboxDao().pendingRows().single().selectionWidening())
    }

    @Test
    fun categoryCreatePersistsItsRequestId() = runBlocking {
        writer.enqueue(
            OutboxKind.CreateCategory(
                name = "Sample",
                provisionalId = -1L,
                requestId = "04e8af73-20b8-48f6-9d30-ca3b15349f83",
            )
        )

        val payload =
            Json.parseToJsonElement(db.outboxDao().pendingRows().single().payloadJson).jsonObject
        assertEquals(
            "04e8af73-20b8-48f6-9d30-ca3b15349f83",
            payload.getValue("request_id").jsonPrimitive.content,
        )
    }

    @Test
    fun mutationRequestsDrainButPersistedLogsDoNotSelfTrigger() = runBlocking {
        writer.enqueue(
            OutboxKind.Log(
                level = "info",
                event = "sample_event",
                fields = emptyMap(),
                timestampMs = 1_000L,
            )
        )
        delay(20L)
        assertEquals(0, drainRequests.get())

        writer.enqueue(OutboxKind.Seen("item-1"))
        delay(20L)
        assertEquals(1, drainRequests.get())
    }

    @Test
    fun interactiveActionsWakeImmediatelyWhilePassiveStateWaitsForDebounce() = runBlocking {
        val localDb = RoomTestSupport.freshDb()
        val scheduler = TestCoroutineScheduler()
        val testScope = TestScope(StandardTestDispatcher(scheduler))
        try {
            val requests = mutableListOf<Boolean>()
            val localPrefs = PreferencesRepo(localDb.preferenceDao(), testScope) { 1_000L }
            val localWriter =
                OutboxWriter(
                    db = localDb,
                    prefs = localPrefs,
                    scope = testScope,
                    onDrainRequested = requests::add,
                    nowMsProvider = { 1_000L },
                    writeDebounceMs = 3_000L,
                )
            testScope.runCurrent()

            localWriter.enqueue(OutboxKind.Like("item-action", OutboxKind.Action.Set))
            assertEquals(listOf(true), requests)

            localWriter.enqueue(OutboxKind.Seen("item-passive"))
            testScope.runCurrent()
            assertEquals(listOf(true), requests)

            scheduler.advanceTimeBy(2_999L)
            testScope.runCurrent()
            assertEquals(listOf(true), requests)

            scheduler.advanceTimeBy(1L)
            testScope.runCurrent()
            assertEquals(listOf(true, false), requests)
        } finally {
            testScope.cancel()
            localDb.close()
        }
    }

    @Test
    fun serverTimeOffsetOwnsMutationTimestamp() = runBlocking {
        prefs.setServerTimeOffsetMs(500L)
        repeat(100) {
            if (prefs.serverTimeOffsetMsSync() == 500L) return@repeat
            delay(5L)
        }

        writer.enqueue(OutboxKind.Like("item-1", OutboxKind.Action.Set))

        val row = db.outboxDao().pendingRows().single()
        val payload = Json.parseToJsonElement(row.payloadJson).jsonObject
        assertEquals(1_500L, row.createdAtMs)
        assertEquals(1_500L, payload.getValue("updated_at_ms").jsonPrimitive.content.toLong())
    }

    @Test
    fun momentsCursorTimestampAdvancesPastRoomAndSameClockWrites() = runBlocking {
        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("following", "first", updatedAtMs = 10_000L))

        writer.recordMomentsCursor("second", 0L, "following", sortAtMs = 200L)
        writer.recordMomentsCursor("third", 0L, "following", sortAtMs = 300L)

        val cursor = requireNotNull(db.momentsCursorDao().get("following"))
        val row = db.outboxDao().pendingRows().single()
        val payload = Json.parseToJsonElement(row.payloadJson).jsonObject
        assertEquals("third", cursor.videoId)
        assertEquals(10_002L, cursor.updatedAtMs)
        assertEquals(10_002L, row.createdAtMs)
        assertEquals("third", payload.getValue("video_id").jsonPrimitive.content)
        assertEquals(
            10_002L,
            payload.getValue("updated_at_ms").jsonPrimitive.content.toLong(),
        )
    }

    @Test
    fun storiesCursorIsMonotonicWithoutEnteringTheOutbox() = runBlocking {
        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("stories", "first", updatedAtMs = 10_000L))

        writer.recordMomentsCursor("second", 0L, "stories", sortAtMs = 200L)

        assertEquals(
            MomentsCursorEntity(
                scope = "stories",
                videoId = "second",
                positionMs = 0L,
                sortAtMs = 200L,
                updatedAtMs = 10_001L,
            ),
            db.momentsCursorDao().get("stories"),
        )
        assertTrue(db.outboxDao().pendingRows().isEmpty())
    }

    @Test
    fun pendingMomentsCursorOverlayKeepsTheNewerTimestamp() = runBlocking {
        writer.recordMomentsCursor("pending", 0L, "all", sortAtMs = 100L)
        val pending = db.outboxDao().pendingRows().single()

        val newerServer = MomentsCursorEntity("all", "server_newer", updatedAtMs = 2_000L)
        db.momentsCursorDao().upsert(newerServer)
        applyOptimisticMutation(db, pending)
        assertEquals(newerServer, db.momentsCursorDao().get("all"))

        val equalServer = MomentsCursorEntity("all", "server_equal", updatedAtMs = 1_000L)
        db.momentsCursorDao().upsert(equalServer)
        applyOptimisticMutation(db, pending)
        assertEquals(equalServer, db.momentsCursorDao().get("all"))

        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("all", "server_older", updatedAtMs = 500L))
        applyOptimisticMutation(db, pending)
        assertEquals("pending", db.momentsCursorDao().get("all")?.videoId)
        assertEquals(1_000L, db.momentsCursorDao().get("all")?.updatedAtMs)
    }

    @Test
    fun differentChannelSettingFieldsRemainIndependent() = runBlocking {
        writer.enqueue(OutboxKind.ChannelSetting("channel-1", "media_only", 1))
        writer.enqueue(OutboxKind.ChannelSetting("channel-1", "max_videos", 50))
        writer.enqueue(OutboxKind.ChannelSetting("channel-1", "include_member_only", 1))

        assertEquals(3, db.outboxDao().countByState("pending"))
        assertEquals(1, db.channelSettingDao().getById("channel-1")?.mediaOnly)
        assertEquals(50, db.channelSettingDao().getById("channel-1")?.maxVideos)
        assertEquals(1, db.channelSettingDao().getById("channel-1")?.includeMemberOnly)
    }

}
