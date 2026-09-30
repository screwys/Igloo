from gallery_dl.extractor import tiktok


class StoryCursor(tiktok.TiktokStoryTimeCursor):
    def next_page(self, data, query_parameters):
        # TikTok returns no cursor when there are no more stories.
        if data.get("statusCode") == 0 and data.get("HasMoreAfter") is False:
            return True
        return super().next_page(data, query_parameters)


class StoryRequest(tiktok.TiktokStoryItemListRequest):
    def cursor_type(self, query_parameters):
        return StoryCursor


class TiktokStoriesExtractor(tiktok.TiktokStoriesExtractor):
    def posts(self):
        user_name = self.groups[0]
        profile_url = f"{self.root}/@{user_name}"
        request = StoryRequest()
        request.execute(self, profile_url, {
            "authorId": self._extract_author_id(profile_url, user_name),
            "loadBackward": "false",
            "count": "5",
        })
        return request.generate_urls(profile_url, self.video, self.photo, self.audio)
