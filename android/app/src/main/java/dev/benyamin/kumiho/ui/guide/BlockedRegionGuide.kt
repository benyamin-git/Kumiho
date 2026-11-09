package dev.benyamin.kumiho.ui.guide

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.EngineClient
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.RegionCheck
import dev.benyamin.kumiho.engine.RegionHost
import dev.benyamin.kumiho.engine.RelayTest
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.ui.theme.Dimens
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * The reusable Blocked region guide (v0.11.0), shared by phone and TV. It is
 * not a wizard step: setup opens it after the consent preflight, Settings →
 * Region opens it, and the dashboard's region error notice opens it.
 *
 * Option 1 (another VPN) leads; the relay is the fallback and keeps the honest
 * boundary that it does not proxy the browser sign-in page. The prepared card
 * counts down the engine's persisted pass, so it survives a restart.
 */
@Composable
fun BlockedRegionGuide(
    container: AppContainer,
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val engine by container.vpnServiceCoordinator.engine.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val requiresSignIn = stringResource(R.string.guide_requires_sign_in)
    val disconnectFirst = stringResource(R.string.guide_disconnect_first)
    val prepareDone = stringResource(R.string.guide_prepare_done)

    var check by remember { mutableStateOf<RegionCheck?>(null) }
    var checkError by remember { mutableStateOf<String?>(null) }
    var checking by remember { mutableStateOf(true) }
    var status by remember { mutableStateOf<EngineStatus?>(null) }
    var settings by remember { mutableStateOf<Settings?>(null) }
    var busy by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    var relayUrl by remember { mutableStateOf("") }
    var relayToken by remember { mutableStateOf("") }
    var relayBusy by remember { mutableStateOf(false) }
    var relayResult by remember { mutableStateOf<RelayTest?>(null) }
    var nowMillis by remember { mutableLongStateOf(System.currentTimeMillis()) }

    suspend fun reload(engine: EngineClient) {
        checking = true
        checkError = null
        runCatching { engine.regionCheck() }
            .onSuccess { check = it }
            .onFailure { checkError = it.message ?: it.javaClass.simpleName }
        checking = false
        runCatching { engine.refreshStatus() }.onSuccess { status = it }
        runCatching { engine.settings() }.onSuccess {
            settings = it
            if (relayUrl.isBlank()) relayUrl = it.relayUrl ?: ""
        }
    }

    LaunchedEffect(engine) {
        engine?.let { reload(it) }
    }
    LaunchedEffect(Unit) {
        while (true) {
            delay(1_000)
            nowMillis = System.currentTimeMillis()
        }
    }

    BackHandler(enabled = true) { onClose() }

    val passExpires = status?.passExpires?.takeIf { it > 0 } ?: 0L
    val remaining = passExpires - nowMillis / 1_000
    val hasPass = remaining > 0
    val authenticated = status?.authenticated == true
    val idle = status?.state == "IDLE"

    Surface(
        modifier = modifier
            .fillMaxSize()
            .testTag("guide_screen"),
        color = MaterialTheme.colorScheme.background,
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space3),
            verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(R.string.guide_title),
                    style = MaterialTheme.typography.headlineSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                TextButton(
                    onClick = onClose,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag("guide_close"),
                ) {
                    Text(stringResource(R.string.guide_close))
                }
            }

            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Column(
                    modifier = Modifier
                        .widthIn(max = Dimens.ContentMaxWidth)
                        .fillMaxWidth(),
                    verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
                ) {
                    GuideCard {
                        Text(
                            text = stringResource(R.string.guide_check_title),
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                        )
                        when {
                            checking -> Row(
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                            ) {
                                CircularProgressIndicator(modifier = Modifier.padding(4.dp))
                                Text(
                                    text = stringResource(R.string.guide_checking),
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            checkError != null -> Text(
                                text = stringResource(R.string.guide_check_error, checkError.orEmpty()),
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.error,
                            )
                            check != null -> {
                                val blocked = check?.blocked.orEmpty()
                                Text(
                                    text = if (blocked.isEmpty()) {
                                        stringResource(R.string.guide_check_clear)
                                    } else {
                                        stringResource(R.string.guide_check_blocked, blocked.size)
                                    },
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                check?.hosts.orEmpty().forEach { host ->
                                    HostRow(host)
                                }
                            }
                        }
                        OutlinedButton(
                            onClick = {
                                engine?.let { client ->
                                    scope.launch { reload(client) }
                                }
                            },
                            enabled = !checking,
                            modifier = Modifier.heightIn(min = Dimens.TouchTarget),
                        ) {
                            Text(stringResource(R.string.guide_recheck))
                        }
                    }

                    if (hasPass || passExpires > 0) {
                        GuideCard {
                            Text(
                                text = stringResource(R.string.guide_pass_title),
                                style = MaterialTheme.typography.titleMedium,
                                fontWeight = FontWeight.SemiBold,
                            )
                            Text(
                                text = if (hasPass) {
                                    stringResource(R.string.guide_pass_ready, shortClock(remaining))
                                } else {
                                    stringResource(R.string.guide_pass_expired)
                                },
                                style = MaterialTheme.typography.bodyMedium,
                                color = if (hasPass) {
                                    MaterialTheme.colorScheme.primary
                                } else {
                                    MaterialTheme.colorScheme.error
                                },
                            )
                            if (hasPass) {
                                Button(
                                    onClick = {
                                        val client = engine ?: return@Button
                                        busy = true
                                        notice = null
                                        scope.launch {
                                            runCatching { client.connect() }
                                                .onSuccess { onClose() }
                                                .onFailure {
                                                    notice = it.message ?: it.javaClass.simpleName
                                                }
                                            busy = false
                                        }
                                    },
                                    enabled = !busy,
                                    modifier = Modifier
                                        .heightIn(min = Dimens.ControlHeight)
                                        .testTag("guide_connect"),
                                ) {
                                    Text(stringResource(R.string.guide_connect))
                                }
                            }
                        }
                    }

                    GuideCard {
                        Text(
                            text = stringResource(R.string.guide_option_one),
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                        )
                        listOf(
                            R.string.guide_option_one_step1,
                            R.string.guide_option_one_step2,
                            R.string.guide_option_one_step3,
                            R.string.guide_option_one_step4,
                            R.string.guide_option_one_step5,
                        ).forEachIndexed { index, res ->
                            Text(
                                text = "${index + 1}. ${stringResource(res)}",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Text(
                            text = stringResource(R.string.guide_option_one_note),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        if (!authenticated) {
                            Text(
                                text = stringResource(R.string.guide_requires_sign_in),
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Button(
                            onClick = {
                                val client = engine ?: return@Button
                                if (!authenticated) {
                                    notice = requiresSignIn
                                    return@Button
                                }
                                if (!idle) {
                                    notice = disconnectFirst
                                    return@Button
                                }
                                busy = true
                                notice = null
                                scope.launch {
                                    runCatching { client.prepare(); client.refreshStatus() }
                                        .onSuccess {
                                            status = it
                                            notice = prepareDone
                                        }
                                        .onFailure {
                                            notice = it.message ?: it.javaClass.simpleName
                                        }
                                    busy = false
                                }
                            },
                            enabled = !busy,
                            modifier = Modifier
                                .heightIn(min = Dimens.ControlHeight)
                                .testTag("guide_prepare"),
                        ) {
                            Text(
                                if (busy) {
                                    stringResource(R.string.guide_preparing)
                                } else {
                                    stringResource(R.string.guide_prepare)
                                },
                            )
                        }
                    }

                    GuideCard {
                        Text(
                            text = stringResource(R.string.guide_option_two),
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.SemiBold,
                        )
                        Text(
                            text = stringResource(R.string.guide_option_two_intro),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        OutlinedTextField(
                            value = relayUrl,
                            onValueChange = { relayUrl = it },
                            label = { Text(stringResource(R.string.guide_relay_url)) },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedButton(
                            onClick = {
                                val client = engine ?: return@OutlinedButton
                                relayBusy = true
                                scope.launch {
                                    runCatching { client.setSetting("relay_url", relayUrl.trim()) }
                                        .onFailure { notice = it.message ?: it.javaClass.simpleName }
                                    relayBusy = false
                                }
                            },
                            enabled = !relayBusy,
                        ) {
                            Text(stringResource(R.string.guide_relay_save_url))
                        }
                        OutlinedTextField(
                            value = relayToken,
                            onValueChange = { relayToken = it },
                            label = { Text(stringResource(R.string.guide_relay_token)) },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedButton(
                            onClick = {
                                val client = engine ?: return@OutlinedButton
                                if (relayToken.isBlank()) return@OutlinedButton
                                relayBusy = true
                                scope.launch {
                                    runCatching { client.setSetting("relay_token", relayToken) }
                                        .onSuccess {
                                            relayToken = ""
                                            settings = client.settings()
                                        }
                                        .onFailure { notice = it.message ?: it.javaClass.simpleName }
                                    relayBusy = false
                                }
                            },
                            enabled = !relayBusy,
                        ) {
                            Text(stringResource(R.string.guide_relay_save_token))
                        }
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                        ) {
                            OutlinedButton(
                                onClick = {
                                    val client = engine ?: return@OutlinedButton
                                    relayBusy = true
                                    relayResult = null
                                    scope.launch {
                                        runCatching { client.relayTest() }
                                            .onSuccess { relayResult = it }
                                            .onFailure {
                                                notice = it.message ?: it.javaClass.simpleName
                                            }
                                        relayBusy = false
                                    }
                                },
                                enabled = !relayBusy,
                            ) {
                                Text(stringResource(R.string.guide_relay_test))
                            }
                            Text(
                                text = relayResultText(relayResult, relayBusy),
                                style = MaterialTheme.typography.bodySmall,
                                color = when {
                                    relayResult?.ok == true -> MaterialTheme.colorScheme.primary
                                    relayResult != null -> MaterialTheme.colorScheme.error
                                    else -> MaterialTheme.colorScheme.onSurfaceVariant
                                },
                            )
                        }
                        if (settings?.relayTokenSet == true) {
                            Text(
                                text = stringResource(R.string.settings_relay_token_set),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Text(
                            text = stringResource(R.string.guide_relay_note),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }

                    Text(
                        text = stringResource(R.string.guide_browser_note),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    if (!authenticated) {
                        Text(
                            text = stringResource(R.string.guide_close_to_sign_in),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    notice?.let {
                        Text(
                            text = it,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun GuideCard(content: @Composable () -> Unit) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.medium,
        color = MaterialTheme.colorScheme.surfaceContainerLow,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column(
            modifier = Modifier.padding(Dimens.Space4),
            verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            content()
        }
    }
}

@Composable
private fun HostRow(host: RegionHost) {
    val tone = when (host.status) {
        "blocked" -> MaterialTheme.colorScheme.error
        "unknown" -> MaterialTheme.colorScheme.tertiary
        else -> MaterialTheme.colorScheme.primary
    }
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        Text(
            text = host.host,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = when (host.status) {
                "blocked" -> stringResource(R.string.guide_status_blocked)
                "unknown" -> stringResource(R.string.guide_status_unknown)
                else -> stringResource(R.string.guide_status_reachable)
            },
            style = MaterialTheme.typography.labelMedium,
            color = tone,
        )
    }
}

@Composable
private fun relayResultText(result: RelayTest?, busy: Boolean): String = when {
    busy -> stringResource(R.string.guide_relay_testing)
    result == null -> ""
    result.ok && result.latencyMs != null ->
        stringResource(R.string.guide_relay_ok_latency, result.latencyMs)
    result.ok -> stringResource(R.string.guide_relay_ok)
    result.status == 401 || result.status == 403 -> stringResource(R.string.guide_relay_rejected)
    else -> stringResource(R.string.guide_relay_unreachable, result.error ?: "HTTP ${result.status}")
}

/** "m:ss" for the pass countdown; never negative. */
private fun shortClock(seconds: Long): String {
    val safe = seconds.coerceAtLeast(0)
    return "${safe / 60}:${(safe % 60).toString().padStart(2, '0')}"
}
