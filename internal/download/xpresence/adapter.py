import json
import logging
import sys
import time

from upstream.api import FleetsAPI, HTTPClient
from upstream.cookies import load_cookies

logging.disable(logging.CRITICAL)


def main():
    request = json.load(sys.stdin)
    ids = request["user_ids"]
    attempts = request["cookies"]
    last_error = {"error": "Login required: X live discovery needs X cookies", "status": 401}
    for index, source in enumerate(attempts):
        client = HTTPClient()
        response_status = 0
        retry_at_ms = 0
        request_started = False

        def observe(response, *args, **kwargs):
            nonlocal response_status, retry_at_ms
            response_status = response.status_code
            reset = response.headers.get("x-rate-limit-reset", "")
            retry = response.headers.get("Retry-After", "")
            if reset.isdigit():
                retry_at_ms = int(reset) * 1000
            elif retry.isdigit():
                retry_at_ms = int(time.time() * 1000) + int(retry) * 1000

        client.session.hooks["response"].append(observe)
        try:
            api = FleetsAPI(client, "fleets", load_cookies(source))
            request_started = True
            result = api.avatar_content(*ids)
            users = result["users"]
            if not isinstance(users, dict):
                raise TypeError("Invalid presence response")
            spaces = []
            for user_id in ids:
                user = users.get(user_id, {})
                room = user.get("spaces", {}).get("live_content", {}).get("audiospace")
                if not room or str(room.get("state", "")).upper() != "RUNNING":
                    continue
                spaces.append({"user_id": user_id, "space_id": room["broadcast_id"],
                               "title": room.get("title", ""),
                               "viewer_count": room.get("total_live_listeners", 0)})
            print(json.dumps({"spaces": spaces, "refresh_seconds": result.get("refresh_delay_secs", 30),
                              "cookie_index": index}))
            return
        except Exception as error:
            if request_started and not response_status:
                causes = []
                cause = error
                while cause is not None:
                    causes.append(str(cause))
                    cause = cause.__cause__ or cause.__context__
                print(json.dumps({"error": "X live discovery request failed", "status": 0,
                                  "detail": "\n".join(causes)}))
                raise SystemExit(1)
            if response_status == 429:
                print(json.dumps({"error": "HTTP 429: X live discovery rate limited", "status": 429,
                                  "retry_at_ms": retry_at_ms}))
                raise SystemExit(1)
            if isinstance(error, (TypeError, ValueError, FileNotFoundError)) and not response_status:
                last_error = {"error": "X cookies could not be loaded", "status": 401}
            elif response_status:
                last_error = {"error": "X live discovery HTTP " + str(response_status), "status": response_status}
            else:
                last_error = {"error": "X live discovery " + type(error).__name__, "status": 0}
        finally:
            client.session.close()
    print(json.dumps(last_error))
    raise SystemExit(1)


main()
