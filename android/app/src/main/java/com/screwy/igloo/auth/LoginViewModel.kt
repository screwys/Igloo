package com.screwy.igloo.auth

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.R
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.net.ServerDiscovery
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * State + input holder for `LoginRoute`. Delegates the actual auth dance to [AuthRepo];
 * translates [AuthRepo.LoginResult] into inline-error text per `07-ui-design-system.md`
 * §4 (login spec).
 *
 * Server URL pre-fills from `AuthRepo.serverUrlSync()`. Public builds use the
 * generic `http://igloo.local:5001` starter unless `DEFAULT_SERVER_URL` is supplied
 * at build time.
 */
class LoginViewModel(
    private val authRepo: AuthRepo,
    private val onLoginSuccess: () -> Unit = {},
    private val serverDiscovery: ServerDiscovery? = null,
) : ViewModel() {

    data class UiState(
        val serverUrl: String = "",
        val username: String = "",
        val password: String = "",
        val status: Status = Status.Idle,
        val discoveryAvailable: Boolean = false,
        val discoveryStatus: DiscoveryStatus = DiscoveryStatus.Idle,
        val discoveredServers: List<String> = emptyList(),
    ) {
        val submitEnabled: Boolean
            get() = status != Status.Loading &&
                serverUrl.isNotBlank() && username.isNotBlank() && password.isNotEmpty()
    }

    sealed class Status {
        data object Idle : Status()
        data object Loading : Status()
        data class Error(@param:StringRes val resId: Int) : Status()
    }

    enum class DiscoveryStatus {
        Idle,
        Scanning,
        NoServers,
    }

    private val _state = MutableStateFlow(
        UiState(
            serverUrl = authRepo.serverUrlSync(),
            discoveryAvailable = serverDiscovery != null,
        ),
    )
    val state: StateFlow<UiState> = _state.asStateFlow()
    private var serverUrlEdited = false

    fun onServerUrlChange(value: String) {
        serverUrlEdited = true
        _state.update {
            it.copy(
                serverUrl = value,
                status = clearErrorOnEdit(it.status),
                discoveryStatus = DiscoveryStatus.Idle,
            )
        }
    }

    fun onUsernameChange(value: String) {
        _state.update { it.copy(username = value, status = clearErrorOnEdit(it.status)) }
    }

    fun onPasswordChange(value: String) {
        _state.update { it.copy(password = value, status = clearErrorOnEdit(it.status)) }
    }

    fun discoverServers() {
        val discovery = serverDiscovery ?: return
        if (_state.value.discoveryStatus == DiscoveryStatus.Scanning) return
        _state.update {
            it.copy(
                discoveryStatus = DiscoveryStatus.Scanning,
                discoveredServers = emptyList(),
            )
        }
        viewModelScope.launch {
            val servers = runCatching { discovery.discover() }
                .getOrDefault(emptyList())
                .distinct()
            _state.update { current ->
                current.copy(
                    serverUrl = if (shouldAutofillDiscoveredServer(current.serverUrl, servers)) {
                        servers.first()
                    } else {
                        current.serverUrl
                    },
                    discoveryStatus = if (servers.isEmpty()) DiscoveryStatus.NoServers else DiscoveryStatus.Idle,
                    discoveredServers = servers,
                )
            }
        }
    }

    fun chooseDiscoveredServer(url: String) {
        onServerUrlChange(url)
    }

    fun onSubmit() {
        val snapshot = _state.value
        if (!snapshot.submitEnabled) return
        _state.update { it.copy(status = Status.Loading) }
        viewModelScope.launch {
            val result = authRepo.login(
                serverUrl = snapshot.serverUrl,
                username = snapshot.username.trim(),
                password = snapshot.password,
            )
            when (result) {
                is AuthRepo.LoginResult.Success -> {
                    _state.update { it.copy(status = Status.Idle, password = "") }
                    onLoginSuccess()
                }
                is AuthRepo.LoginResult.BadCredentials ->
                    _state.update { it.copy(status = Status.Error(R.string.login_error_invalid_credentials)) }
                is AuthRepo.LoginResult.NetworkError ->
                    _state.update { it.copy(status = Status.Error(R.string.login_error_reach_server)) }
                is AuthRepo.LoginResult.ServerError ->
                    _state.update { it.copy(status = Status.Error(R.string.login_error_server_try_again)) }
            }
        }
    }

    private fun clearErrorOnEdit(current: Status): Status =
        if (current is Status.Error) Status.Idle else current

    private fun shouldAutofillDiscoveredServer(currentUrl: String, servers: List<String>): Boolean {
        if (servers.isEmpty() || serverUrlEdited) return false
        return currentUrl.isBlank() || currentUrl == PreferencesRepo.Defaults.BUILTIN_SERVER_URL
    }
}
