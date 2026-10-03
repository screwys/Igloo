package com.screwy.igloo.data

import android.content.Context
import androidx.room.Room
import androidx.sqlite.driver.bundled.BundledSQLiteDriver
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.runBlocking

/** Fresh bundled-SQLite databases for JVM checks under Robolectric. */
object RoomTestSupport {
    fun freshDb(): IglooDatabase {
        val ctx: Context = ApplicationProvider.getApplicationContext()
        return Room.inMemoryDatabaseBuilder(ctx, IglooDatabase::class.java)
            .setDriver(BundledSQLiteDriver())
            .allowMainThreadQueries()
            .build()
    }

    fun closeAfterScope(scope: CoroutineScope, db: IglooDatabase) {
        runBlocking {
            scope.coroutineContext[Job]?.cancelAndJoin()
        }
        db.close()
    }
}
