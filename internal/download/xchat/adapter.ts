import {
	bootstrapBroadcast,
	connectBroadcastChat,
	fetchInitialHistory,
	HttpError,
} from "./upstream/twitter-chat-api.ts";
import type { BroadcastChatMessage } from "./upstream/types.ts";

function emit(message: BroadcastChatMessage): void {
	console.log(JSON.stringify({
		id: message.uuid,
		author_id: message.remoteId ?? message.username,
		author: message.displayName,
		text: message.text,
		timestamp_ms: message.timestampMs,
	}));
}

async function main(): Promise<void> {
	const heartbeat = setInterval(() => console.log('{"type":"ping"}'), 10_000);
	try {
		const bootstrap = await bootstrapBroadcast(Deno.args[0] ?? "");
		for (const message of await fetchInitialHistory(bootstrap)) emit(message);
		console.log('{"type":"ready"}');
		if (Deno.args[1] === "history") return;
		await new Promise<void>((resolve, reject) => {
			const connection = connectBroadcastChat(bootstrap, emit, (error) => {
				connection.close();
				reject(error);
			}, resolve);
		});
	} finally {
		clearInterval(heartbeat);
	}
}

main().catch((error) => {
	console.error(error instanceof HttpError ? `HTTP ${error.status}` : error.message);
	Deno.exit(1);
});
