package db

func schemaSearchStatements() []string {
	return []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS search_channels_fts USING fts5(
			channel_id_pk UNINDEXED,
			name,
			source_id,
			display_name,
			handle,
			tokenize = 'unicode61'
		)`,
		`DROP TRIGGER IF EXISTS trg_search_channel_profiles_ai`,
		`DROP TRIGGER IF EXISTS trg_search_channel_profiles_au`,
		`DROP TRIGGER IF EXISTS trg_search_channel_profiles_ad`,
		`DROP TRIGGER IF EXISTS trg_search_videos_ai`,
		`DROP TRIGGER IF EXISTS trg_search_videos_au`,
		`DROP TRIGGER IF EXISTS trg_search_video_channels_au`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_channels_ai AFTER INSERT ON channels BEGIN
			INSERT INTO search_channels_fts(rowid, channel_id_pk, name, source_id, display_name, handle)
			VALUES (
				new.id,
				new.channel_id,
				COALESCE(new.name, ''),
				COALESCE(new.source_id, ''),
				COALESCE((SELECT display_name FROM channel_profiles WHERE channel_id = new.channel_id AND COALESCE(tombstone, 0) = 0), ''),
				COALESCE((SELECT handle FROM channel_profiles WHERE channel_id = new.channel_id AND COALESCE(tombstone, 0) = 0), '')
			);
		END`,
		searchChannelsUpdateTriggerStatement(),
		`CREATE TRIGGER IF NOT EXISTS trg_search_channels_ad AFTER DELETE ON channels BEGIN
			DELETE FROM search_channels_fts WHERE rowid = old.id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_channel_profiles_ai AFTER INSERT ON channel_profiles BEGIN
			DELETE FROM search_channels_fts WHERE rowid = (SELECT id FROM channels WHERE channel_id = new.channel_id);
			INSERT INTO search_channels_fts(rowid, channel_id_pk, name, source_id, display_name, handle)
			SELECT c.id, c.channel_id, COALESCE(c.name, ''), COALESCE(c.source_id, ''),
			       COALESCE(cp.display_name, ''), COALESCE(cp.handle, '')
			FROM channels c
			LEFT JOIN channel_profiles cp ON cp.channel_id = c.channel_id AND COALESCE(cp.tombstone, 0) = 0
			WHERE c.channel_id = new.channel_id;
			DELETE FROM search_videos_fts WHERE rowid IN (SELECT id FROM videos WHERE channel_id = new.channel_id);
			INSERT INTO search_videos_fts(rowid, video_id_pk, title, dearrow_title, dearrow_title_casual, channel_name)
			SELECT v.id, v.video_id, COALESCE(v.title, ''), COALESCE(v.dearrow_title, ''),
			       COALESCE(v.dearrow_title_casual, ''), COALESCE(new.display_name, '')
			FROM videos v
			WHERE v.channel_id = new.channel_id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_channel_profiles_au AFTER UPDATE ON channel_profiles
		WHEN old.channel_id IS NOT new.channel_id
		  OR old.display_name IS NOT new.display_name
		  OR old.handle IS NOT new.handle
		  OR old.tombstone IS NOT new.tombstone
		BEGIN
			DELETE FROM search_channels_fts WHERE rowid = (SELECT id FROM channels WHERE channel_id = new.channel_id);
			INSERT INTO search_channels_fts(rowid, channel_id_pk, name, source_id, display_name, handle)
			SELECT c.id, c.channel_id, COALESCE(c.name, ''), COALESCE(c.source_id, ''),
			       COALESCE(cp.display_name, ''), COALESCE(cp.handle, '')
			FROM channels c
			LEFT JOIN channel_profiles cp ON cp.channel_id = c.channel_id AND COALESCE(cp.tombstone, 0) = 0
			WHERE c.channel_id = new.channel_id;
			DELETE FROM search_videos_fts WHERE rowid IN (SELECT id FROM videos WHERE channel_id = new.channel_id);
			INSERT INTO search_videos_fts(rowid, video_id_pk, title, dearrow_title, dearrow_title_casual, channel_name)
			SELECT v.id, v.video_id, COALESCE(v.title, ''), COALESCE(v.dearrow_title, ''),
			       COALESCE(v.dearrow_title_casual, ''), COALESCE(new.display_name, '')
			FROM videos v
			WHERE v.channel_id = new.channel_id;
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_channel_profiles_ad AFTER DELETE ON channel_profiles BEGIN
			DELETE FROM search_channels_fts WHERE rowid = (SELECT id FROM channels WHERE channel_id = old.channel_id);
			INSERT INTO search_channels_fts(rowid, channel_id_pk, name, source_id, display_name, handle)
			SELECT c.id, c.channel_id, COALESCE(c.name, ''), COALESCE(c.source_id, ''), '', ''
			FROM channels c
			WHERE c.channel_id = old.channel_id;
			DELETE FROM search_videos_fts WHERE rowid IN (SELECT id FROM videos WHERE channel_id = old.channel_id);
			INSERT INTO search_videos_fts(rowid, video_id_pk, title, dearrow_title, dearrow_title_casual, channel_name)
			SELECT v.id, v.video_id, COALESCE(v.title, ''), COALESCE(v.dearrow_title, ''),
			       COALESCE(v.dearrow_title_casual, ''), ''
			FROM videos v
			WHERE v.channel_id = old.channel_id;
		END`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS search_videos_fts USING fts5(
			video_id_pk UNINDEXED,
			title,
			dearrow_title,
			dearrow_title_casual,
			channel_name,
			tokenize = 'unicode61'
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_videos_ai AFTER INSERT ON videos BEGIN
			INSERT INTO search_videos_fts(rowid, video_id_pk, title, dearrow_title, dearrow_title_casual, channel_name)
			VALUES (
				new.id,
				new.video_id,
				COALESCE(new.title, ''),
				COALESCE(new.dearrow_title, ''),
				COALESCE(new.dearrow_title_casual, ''),
				COALESCE((SELECT display_name FROM channel_profiles WHERE channel_id = new.channel_id), '')
			);
		END`,
		searchVideosUpdateTriggerStatement(),
		`CREATE TRIGGER IF NOT EXISTS trg_search_videos_ad AFTER DELETE ON videos BEGIN
			DELETE FROM search_videos_fts WHERE rowid = old.id;
		END`,
	}
}

func searchChannelsUpdateTriggerStatement() string {
	return `CREATE TRIGGER IF NOT EXISTS trg_search_channels_au
		AFTER UPDATE OF id, channel_id, name, source_id ON channels
		WHEN old.id IS NOT new.id
		  OR old.channel_id IS NOT new.channel_id
		  OR old.name IS NOT new.name
		  OR old.source_id IS NOT new.source_id
		BEGIN
			DELETE FROM search_channels_fts WHERE rowid = old.id;
			INSERT INTO search_channels_fts(rowid, channel_id_pk, name, source_id, display_name, handle)
			VALUES (
				new.id,
				new.channel_id,
				COALESCE(new.name, ''),
				COALESCE(new.source_id, ''),
				COALESCE((SELECT display_name FROM channel_profiles WHERE channel_id = new.channel_id AND COALESCE(tombstone, 0) = 0), ''),
				COALESCE((SELECT handle FROM channel_profiles WHERE channel_id = new.channel_id AND COALESCE(tombstone, 0) = 0), '')
			);
		END`
}

func searchVideosUpdateTriggerStatement() string {
	return `CREATE TRIGGER IF NOT EXISTS trg_search_videos_au
		AFTER UPDATE OF id, video_id, channel_id, title, dearrow_title, dearrow_title_casual ON videos
		WHEN old.id IS NOT new.id
		  OR old.video_id IS NOT new.video_id
		  OR old.channel_id IS NOT new.channel_id
		  OR old.title IS NOT new.title
		  OR old.dearrow_title IS NOT new.dearrow_title
		  OR old.dearrow_title_casual IS NOT new.dearrow_title_casual
		BEGIN
			DELETE FROM search_videos_fts WHERE rowid = old.id;
			INSERT INTO search_videos_fts(rowid, video_id_pk, title, dearrow_title, dearrow_title_casual, channel_name)
			VALUES (
				new.id,
				new.video_id,
				COALESCE(new.title, ''),
				COALESCE(new.dearrow_title, ''),
				COALESCE(new.dearrow_title_casual, ''),
				COALESCE((SELECT display_name FROM channel_profiles WHERE channel_id = new.channel_id), '')
			);
		END`
}

func schemaSubstringSearchStatements() []string {
	return []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS search_feed_text_fts USING fts5(
			body_text, content = 'feed_items', content_rowid = 'rowid',
			tokenize = 'trigram', detail = 'none', columnsize = 0
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_feed_text_ai AFTER INSERT ON feed_items BEGIN
			INSERT INTO search_feed_text_fts(rowid, body_text) VALUES (new.rowid, new.body_text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_feed_text_ad AFTER DELETE ON feed_items BEGIN
			INSERT INTO search_feed_text_fts(search_feed_text_fts, rowid, body_text)
			VALUES ('delete', old.rowid, old.body_text);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_feed_text_au AFTER UPDATE ON feed_items
		WHEN old.rowid IS NOT new.rowid OR old.body_text IS NOT new.body_text BEGIN
			INSERT INTO search_feed_text_fts(search_feed_text_fts, rowid, body_text)
			VALUES ('delete', old.rowid, old.body_text);
			INSERT INTO search_feed_text_fts(rowid, body_text) VALUES (new.rowid, new.body_text);
		END`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS search_profile_text_fts USING fts5(
			handle, display_name, content = 'channel_profiles', content_rowid = 'rowid',
			tokenize = 'trigram', detail = 'none', columnsize = 0
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_profile_text_ai AFTER INSERT ON channel_profiles BEGIN
			INSERT INTO search_profile_text_fts(rowid, handle, display_name)
			VALUES (new.rowid, new.handle, new.display_name);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_profile_text_ad AFTER DELETE ON channel_profiles BEGIN
			INSERT INTO search_profile_text_fts(search_profile_text_fts, rowid, handle, display_name)
			VALUES ('delete', old.rowid, old.handle, old.display_name);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_search_profile_text_au AFTER UPDATE ON channel_profiles
		WHEN old.rowid IS NOT new.rowid OR old.handle IS NOT new.handle OR old.display_name IS NOT new.display_name BEGIN
			INSERT INTO search_profile_text_fts(search_profile_text_fts, rowid, handle, display_name)
			VALUES ('delete', old.rowid, old.handle, old.display_name);
			INSERT INTO search_profile_text_fts(rowid, handle, display_name)
			VALUES (new.rowid, new.handle, new.display_name);
		END`,
	}
}
