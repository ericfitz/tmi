# Prompts to generate a collaborative editing plan

Having already laid the ground work - REST API, server-side storage, functioning websocket hub, domain model on client with PATCH updates for single- or multi-cell changes - I wanted to complete the collaborative editing feature. Here's how I created and iterated on the plan with Claude Code.

## 1

**Prompt #1 is where initially I am just asking for a plan to iterated on; I pointed out some of the problems the plan had to solve just to give it a little direction. Other context in the repo laid out the vision for collaborative editing.**

Now I want to come up with a plan for enabling actual collaborative editing. We can set up collaboration sessions on a diagram, we can connect multiple users to the websocket collaboration session. We have to figure out:

1. how should each client transition from using REST APIs to websocket messages,
2. what messages should they send when they update one or more cells in a diagram,
3. how the server will "broadcast" those messages to all connected clients (with split horizon to avoid echoing back messages to the original client),
4. how clients should react/what they should do when they receive messages,
5. how to handle out-of-order messages or conflicting messages, e.g. an update after a delete, and
6. whether any new messages are needed.

We need to develop and refine a plan, and write it down. Part of the plan will be to update tmi-asyncapi.yaml, part of the plan will be to write documentation for client developers, and of course implementation and testing plans. Please take a first stab at the plan and then review it with me.

## 2

**Prompt #2 is where I want to iterate on the plan to shape it more to my liking. I am mainly reacting to its proposal (e.g. "take out the transition plan and schedule") but also changing direction to solve problems the way I want them solved (e.g. instead of sharing everyone's cursor, let's make a "presenter" feature). I also give it more context about what we have to work with.**

ok let's refine the plan a little bit - first, we don't need a phased transition plan - the application has not launched yet and we control the only client and only server so there is no need to maintain backwards compatibility. If there are simple opportunities to migrate a little functionality from REST to websocket, then we should do that as a way to work out the kinks without having to worry about changing everything all at once, but that is nice-to-have, not must-have.

Of note is that the client already has a layered design for graph changes. There is a "domain" layer, that enforces business rules and so forth, and a graph layer, which directly interacts with the canvas. All incoming and outgoing API messages go through the domain layer. The client also already has the ability to handle operations for multiple cells atomically at the domain layer, and PATCH requests should already reflect this.

We need each message to identify the user who actuated/initiated the message, because we want to display the user name when we apply an update to the graph, that came from a remote user. Each user has their own cursor and selection, and it would be very difficult to make a graceful way to show selection and cursors for all active users. Instead, we should have a "presenter" (initially the user who initiated the session). The session "owner" should always be able to "take" presenter mode. Other users should be able to "request" presenter mode, and the "owner" should have to approve in order to transition. We can have cursor messages and selection messages, but they'll only be broadcast for the active presenter. Well behaved clients will not transmit these messages when their user is not the presenter. "Selection" in our graph library is 0 or more cells.

My initial opinion is that conflict resolution should be operation-based, e.g. we will discard messages that represent impossible state transitions, like cell updates after a delete. I don't care about cell position or size conflicts; we can use last writer wins, but in update-after-delete and cases we need a message back to the sender(s) of the discarded message(s) to update them with the relevant state, and for last-writer-wins we need to update all clients with final state. However I would like advice and criticism, pros and cons of this approach, alternative approaches, and suggested approach.

I don't want a schedule or estimated times, just a plan with phases where we can pause at the end of any phase and have an app that still works, even if all functionality is not complete or transitioned.

I would like a detailed plan for operation sequencing and history tracking, and I'd like to make sure that our history only tracks mutations to the graph, and does not include things like selection, pointer position, etc. We already have a local history mechanism with undo and redo buttons and a local history service on each client. We can easily disable this while in collaboration mode and connect "undo" and "redo" buttons to messages that will cause the server to do something. We need a mechanism for the client to detect if it is out of sync and request a resync. In fact, this might be used for initializing the diagram. Resync could be just GET /threat_models/{id}/diagrams/{diagram_id}, unless there is a good reason to do this in the websocket.

To the greatest extent possible, I'd like our websocket messages describing changes to the graph to resemble our REST PATCH operations, and I'd like them to be focused on cells or arrays of cells, and describe the changes to the cell(s) in question. Our current messages already support batches (we patch cells via the array of cells in a diagram), so this might also make validation and batching easier if we find a way to refactor our existing PATCH /threat_models/{id}/diagrams/{diagram_id} code so it can be leveraged by WS as well as websockets- again, nice-to-have, but it also means that we don't have distinct sets of bugs and separate behaviors for REST vs. websocket mode.

I don't want to try for maximum effiency, we will tend to have small sets of collaborators on any given diagram, so we should bias towards protocol simplicity over brevity, with the caveat that we already have working REST APIs and will minimize client changes by leveraging the marshaling code we already have, just into different messages/transport.

I'd like more detail on why we need ACK messages and what we'd do with them if we got one (or failed to get one within a time window).

I do NOT want to get rid of any of our current REST endpoints; we may have automation clients that choose to use those endpoints for diagram creation and manipulation.

The server needs an authorization filter on websocket sessions, and we need to discard mutation messages from users with only "reader" access, send them a message that they're unauthorized to do that, and send them a message to put their graph back into the correct global state by "undo-ing" their change or by selectively updating the state of the changed cell(s) to whatever the current global state is for those cells. Read-only users should still be able to be promoted to "presenter" and have pointer and selection messages broadcast.

Please update the plan given these details. Where I have expressed a preference for behavior or implementation, please jot that preference down in the plan. Please update the file and then we can review again. Don't start implementation yet.

## 3

**Prompt #3 is fine tuning. Also I continue to point out edge cases and how I want them handled. Plus a request for clarification of something I didn't understand why we needed.**

we should change response messages to presenter requests. In the "deny" case, we just need to send a deny to the requester. In the "grant" case, the owner needs to send a "change presenter" message to the server (reject from anyone except owner), and then the server needs to broadcast a "current presenter" message to all connected users. Also: edge case: if the current presenter leaves the session, the server should automatically revert to presenter=owner, and broadcast the appropriate "current presenter" message.

I don't understand the difference between "operation_id" and "sequence_number" in the core message structure, and what they are used for- please explain this in more detail. We should make it clear with undo/redo messages to the server, that the server will only accept undo and redo messages from users with mutation privileges, and that the server will respond to authorized undo/redo requests with a broadcast of the new state (in the short term we could just broadcast a message like "history operation; resync now").

We need really clear guidance to clients to NOT echo mutation events when they update their local graph based on websocket messages, or after resyncs.

The server should not broadcast messages for mutating changes that didn't actually change any actual state. This will allow the server to mitigate the effects of a poorly behaved client who receives a state change message, changes state, and then accidentally broadcasts a message indicating the state change they made locally.

## 4

**Prompt #4 is final fine tuning. I changed the order a little so we could start client work in parallel, and I added specific requirements for diagnosability, which I know from experience we will need to get the client communicating with the server.**

I think that we need to update the AsyncAPI schema before we actually start code implementation. We should also see if there are Go libraries for AsyncAPI that can do some automatic code generation for us.

We need detailed debug logging on the server for mutation operations, so that we can understand what caused (why we are performing) each mutation event and exactly what we're changing - this will be critical for debugging.

We also need a debug logging facility to record all websocket messages in a session; but we need to be able to turn that on and off easily at runtime; we will need it for debugging, but it will be too noisy to leave on all the time for all sessions.

Please update the doc with this information, and I will do one final review before I approve implementation.
