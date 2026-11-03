package com.screwy.igloo.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.auth.AccountSessionActions
import com.screwy.igloo.auth.LogoutReason
import com.screwy.igloo.data.PreferencesRepo
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch

class AccountSettingsViewModel(
    private val prefs: PreferencesRepo,
    private val authRepo: AccountSessionActions,
) : ViewModel() {

    val serverUrl: StateFlow<String> =
        authRepo.serverUrlFlow

    val localServerUrl: StateFlow<String> = authRepo.localServerUrlFlow

    fun setLocalServerUrl(value: String) = authRepo.updateLocalServerUrl(value)

    fun setServerUrl(value: String) {
        viewModelScope.launch {
            prefs.setServerUrl(value)
            authRepo.updateServerUrl(value)
        }
    }

    fun logout() {
        viewModelScope.launch { authRepo.logout(LogoutReason.UserInitiated) }
    }

}
