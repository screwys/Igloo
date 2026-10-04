import asyncio
import json
import logging
import sys
import time
from urllib.parse import urlsplit

from TikTokLive import TikTokLiveClient
from TikTokLive.events import CommentEvent


class NoLiveTransportError(Exception):
    pass


def error_message(error):
    return str(error) if isinstance(error, NoLiveTransportError) else type(error).__name__


def write_json(value):
    print(json.dumps(value), flush=True)


def playback_formats(room):
    stream = room["stream_url"]
    data = json.loads(stream["live_core_sdk_data"]["pull_data"]["stream_data"])
    formats = []
    transports = set()
    for quality, entry in data["data"].items():
        main = entry["main"]
        params = json.loads(main.get("sdk_params") or "{}")
        for transport in ("hls", "flv", "cmaf", "dash"):
            if main.get(transport):
                transports.add(transport)
        for transport in ("hls", "flv"):
            url = main.get(transport)
            if not url:
                continue
            formats.append({
                "format_id": quality + "-" + transport,
                "url": url,
                "manifest_url": url if transport == "hls" else "",
                "protocol": "m3u8_native" if transport == "hls" else urlsplit(url).scheme,
                "ext": "mp4" if transport == "hls" else "flv",
                "vcodec": "none" if quality == "ao" else params.get("VCodec", ""),
            })
    if not formats:
        raise NoLiveTransportError("No HLS or FLV stream; available transports: " + ", ".join(sorted(transports)))
    formats.sort(key=lambda entry: entry["vcodec"] == "none")
    return formats


async def resolve(handle):
    client = TikTokLiveClient(unique_id=handle)
    client.logger.disabled = True
    try:
        if not await client.is_live():
            return None
        room = await client.web.fetch_room_info(unique_id=handle)
        room_id = str(room["id_str"])
        owner = room["owner"]
        cover_urls = room.get("cover", {}).get("url_list", [])
        thumbnail = cover_urls[0] if cover_urls else ""
        source_url = "https://www.tiktok.com/@" + handle + "/live"
        return {
            "channel_id": "tiktok_" + handle,
            "room_id": room_id,
            "handle": handle,
            "title": room.get("title", ""),
            "thumbnail_url": thumbnail,
            "viewer_count": room.get("user_count", 0),
            "playback": {
                "id": room_id,
                "title": room.get("title", ""),
                "channel_id": "tiktok_" + handle,
                "channel": owner.get("nickname", ""),
                "channel_url": "https://www.tiktok.com/@" + handle,
                "uploader_id": handle,
                "thumbnail": thumbnail,
                "is_live": True,
                "formats": playback_formats(room),
                "live_status": "is_live",
                "webpage_url": source_url,
            },
        }
    finally:
        await client.web.close()


async def batch(handles):
    semaphore = asyncio.Semaphore(6)

    async def fetch(handle):
        async with semaphore:
            try:
                info = await resolve(handle)
                write_json({"handle": handle, "info": info})
            except Exception as error:
                write_json({"handle": handle, "error": error_message(error)})

    await asyncio.gather(*(fetch(handle) for handle in handles))


async def chat(handle):
    client = TikTokLiveClient(unique_id=handle)
    client.logger.disabled = True

    @client.on(CommentEvent)
    async def comment(event):
        common = event.common
        user = event.user
        write_json({
            "id": str(common.msg_id) if common else "",
            "author_id": str(user.id) if user else "",
            "author": user.nickname if user else "",
            "text": event.comment,
            "timestamp_ms": common.create_time if common and common.create_time else time.time_ns() // 1_000_000,
        })

    async def heartbeat():
        while True:
            await asyncio.sleep(10)
            write_json({"type": "ping"})

    heartbeat_task = asyncio.create_task(heartbeat())
    try:
        await client.connect()
    finally:
        heartbeat_task.cancel()
        await asyncio.gather(heartbeat_task, return_exceptions=True)
        await client.disconnect()
        await client.web.close()


async def main():
    logging.disable(logging.CRITICAL)
    mode = sys.argv[1]
    if mode == "batch":
        await batch(json.load(sys.stdin))
    elif mode == "chat":
        await chat(sys.argv[2])
    else:
        write_json(await resolve(sys.argv[2]))


try:
    asyncio.run(main())
except Exception as error:
    print(error_message(error), file=sys.stderr)
    sys.exit(1)
