import 'dart:async';

import 'package:flutter/material.dart';

import '../app_state.dart';
import '../models/chat_message.dart';
import '../models/chat_stream_event.dart';
import '../models/model_info.dart';
import '../services/api_client.dart';
import '../widgets/chat_bubble.dart';
import '../widgets/connection_banner.dart';
import '../widgets/reauth_prompt.dart';

/// Streaming chat over `/v1/chat/completions`. Sends `stream: true`, renders
/// SSE deltas as they arrive, shows the route chip row once headers land,
/// and — on a mid-stream failure — keeps whatever text already streamed in
/// and appends an error strip rather than losing the partial reply.
class ChatScreen extends StatefulWidget {
  const ChatScreen({super.key});

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final _messages = <ChatMessage>[];
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  StreamSubscription<ChatStreamEvent>? _sub;

  List<ModelInfo> _models = const [ModelInfo(id: 'auto', ownedBy: 'prism')];
  String _selectedModel = 'auto';
  bool _sending = false;
  bool _showReauth = false;
  bool _loadingModels = false;
  String? _bannerMessage;

  AppState get _appState => AppStateScope.of(context);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _loadModels());
  }

  @override
  void dispose() {
    _sub?.cancel();
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadModels() async {
    setState(() => _loadingModels = true);
    try {
      final models = await _appState.client.fetchModels();
      if (!mounted) return;
      setState(() {
        _models = models.isEmpty ? _models : models;
        final defaultId = _appState.settings.defaultModelId;
        _selectedModel = _models.any((m) => m.id == defaultId) ? defaultId : 'auto';
        _bannerMessage = null;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      if (!e.isAuthError) {
        setState(() => _bannerMessage = 'Could not load the model list: ${e.message}');
      }
    } catch (e) {
      if (!mounted) return;
      setState(() => _bannerMessage = 'Could not load the model list: $e');
    } finally {
      if (mounted) setState(() => _loadingModels = false);
    }
  }

  void _clearConversation() {
    _sub?.cancel();
    setState(() {
      _messages.clear();
      _sending = false;
      _showReauth = false;
    });
  }

  void _send() {
    final text = _inputCtrl.text.trim();
    if (text.isEmpty || _sending) return;
    _inputCtrl.clear();
    setState(() {
      _messages.add(ChatMessage.user(text));
      _messages.add(ChatMessage.assistantPlaceholder());
      _sending = true;
      _showReauth = false;
    });
    _streamReply();
  }

  void _retryLast() {
    if (_messages.isEmpty) return;
    final placeholder = _messages.last;
    if (placeholder.role != ChatRole.assistant) return;
    setState(() {
      placeholder.content = '';
      placeholder.error = null;
      placeholder.route = null;
      placeholder.isStreaming = true;
      _sending = true;
      _showReauth = false;
    });
    _streamReply();
  }

  void _streamReply() {
    final placeholder = _messages.last;
    final history = _messages
        .sublist(0, _messages.length - 1)
        .where((m) => m.role == ChatRole.user || m.content.trim().isNotEmpty)
        .toList(growable: false);

    _sub?.cancel();
    _sub = _appState.client
        .streamChatCompletion(model: _selectedModel, messages: history)
        .listen(
      (event) {
        if (!mounted) return;
        setState(() {
          switch (event) {
            case ChatRouteEvent(:final route):
              placeholder.route = route;
            case ChatDeltaEvent(:final text):
              placeholder.content += text;
            case ChatDoneEvent():
              placeholder.isStreaming = false;
              _sending = false;
            case ChatErrorEvent(:final message, :final statusCode):
              placeholder.isStreaming = false;
              placeholder.error = message;
              _sending = false;
              if (statusCode == 401) _showReauth = true;
          }
        });
        _scrollToEnd();
      },
      // _sending is reset above, on the ChatDoneEvent/ChatErrorEvent data
      // event rather than here: streamChatCompletion's async* generator
      // `return`s right after yielding one of those, so the exposed
      // stream's actual done notification can lag a beat behind the event
      // that already told the UI the reply is over (it has to propagate
      // back through the SSE line-splitting/decoding transformers first).
      // Gating "can the user send another message" on that extra lag would
      // leave the send button spuriously disabled for longer than
      // necessary; the two terminal data events are the authoritative
      // signal.
    );
  }

  Future<void> _submitReauth(String apiKey) async {
    await _appState.updateApiKey(apiKey);
    if (!mounted) return;
    setState(() => _showReauth = false);
  }

  void _scrollToEnd() {
    if (!_scrollCtrl.hasClients) return;
    _scrollCtrl.animateTo(
      _scrollCtrl.position.maxScrollExtent + 80,
      duration: const Duration(milliseconds: 150),
      curve: Curves.easeOut,
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Chat'),
        actions: [
          IconButton(
            icon: const Icon(Icons.delete_outline),
            tooltip: 'Clear conversation',
            onPressed: _messages.isEmpty ? null : _clearConversation,
          ),
        ],
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(48),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: _buildModelPicker(),
          ),
        ),
      ),
      body: Column(
        children: [
          if (_bannerMessage != null)
            ConnectionBanner(
              message: _bannerMessage!,
              onRetry: _loadModels,
              retrying: _loadingModels,
            ),
          if (_showReauth)
            ReauthPrompt(
              message: 'Your API key was rejected by the gateway.',
              onSubmit: _submitReauth,
            ),
          Expanded(
            child: _messages.isEmpty ? const _EmptyChatState() : _buildMessageList(),
          ),
          const Divider(height: 1),
          _buildInputBar(),
        ],
      ),
    );
  }

  Widget _buildModelPicker() {
    return DropdownButtonHideUnderline(
      child: DropdownButton<String>(
        key: const Key('chat_model_picker'),
        value: _selectedModel,
        icon: const Icon(Icons.expand_more, size: 18),
        isDense: true,
        items: [
          for (final m in _models)
            DropdownMenuItem(value: m.id, child: Text(m.id)),
        ],
        onChanged: _sending
            ? null
            : (v) {
                if (v != null) setState(() => _selectedModel = v);
              },
      ),
    );
  }

  Widget _buildMessageList() {
    return ListView.builder(
      key: const Key('chat_message_list'),
      controller: _scrollCtrl,
      padding: const EdgeInsets.all(12),
      itemCount: _messages.length,
      itemBuilder: (context, i) {
        final message = _messages[i];
        final isLast = i == _messages.length - 1;
        return ChatBubble(
          message: message,
          onRetry: (isLast && message.error != null) ? _retryLast : null,
        );
      },
    );
  }

  Widget _buildInputBar() {
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Expanded(
              child: TextField(
                key: const Key('chat_input_field'),
                controller: _inputCtrl,
                minLines: 1,
                maxLines: 5,
                textInputAction: TextInputAction.send,
                onSubmitted: (_) => _send(),
                decoration: const InputDecoration(
                  hintText: 'Message DL Prism…',
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.all(Radius.circular(24)),
                  ),
                  contentPadding: EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                ),
              ),
            ),
            const SizedBox(width: 8),
            IconButton.filled(
              key: const Key('chat_send_button'),
              onPressed: _sending ? null : _send,
              icon: _sending
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.send),
            ),
          ],
        ),
      ),
    );
  }
}

class _EmptyChatState extends StatelessWidget {
  const _EmptyChatState();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.chat_bubble_outline, size: 40, color: theme.colorScheme.outline),
            const SizedBox(height: 12),
            Text(
              'Ask anything — DL Prism routes it to the best model for the job.',
              textAlign: TextAlign.center,
              style: TextStyle(color: theme.colorScheme.onSurfaceVariant),
            ),
          ],
        ),
      ),
    );
  }
}
