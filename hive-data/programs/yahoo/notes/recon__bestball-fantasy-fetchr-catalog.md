---
program_id: yahoo
document_type: note
claimed_scope_status: unknown
classification: internal
source: intigriti policy (yahoo) — 2026-09-08
collected_at: 2026-09-12T00:00:00Z
tags: [recon, yahoo]
asset_refs: [apis.mail.yahoo.com]
---
# Best Ball / Fantasy — catálogo fetchr (distilado 2026-09-12)

Host: bestball.fantasysports.yahoo.com (VIVO 200, ATS+Envoy, React tdv2-app-fantasy)
xhrPath (fetchr): /tdfan/api/resource/<service>;<matrix-params>?<query>
Auth: crumb p/ escrita; wall inconsistente entre serviços (ver notas).
Registro de serviço é POR-HOST (o catálogo é o superset compartilhado dos apps fantasy).

## Wall unauth observado (bestball host)
- bestball.myLeagues            -> 401 (fail-closed)      registrado
- wallet.bethistory (+guidOverride) -> 503 request-failed  registrado, alcança backend BetMGM
- wallet.bethistorysummary      -> 503                     registrado
- wallet.product                -> 500                     registrado
- wallet.subscriptions          -> 200 (vazio, no-user)    registrado, FAIL-OPEN shape
- fantasyread.* / tourney.* / progrss.* -> 400 not registered (outros hosts)

## Leads BOLA/IDOR (ranqueados) — TODOS precisam sessão pra provar
1. [MONEY+PII] wallet.bethistory / wallet.bethistorysummary
   path /sportsbook/betmgm/user/betHistory ; param guidOverride (client-controlado)
   -> hipótese: guidOverride=<guid alheio> lê histórico de apostas de outro user.
2. [MONEY] tourney.* (host tournament., auth-walled): bracketPicks(teamId,teamKey),
   groupStandings/groupMembers/groupSettings(groupId,groupKey,invitationKey),
   editorialTeam(teamKey), smartAdsTeam(teamId,teamKey,useLogin<-toggle auth §7)
3. [PII] progrss.contacts /user/{guid}/contacts;out=name,email,image (cross-user)
4. [LOGIC] graphite.bettingRestriction userStateAbbr client-controlado -> bypass geo aposta
5. [LOW] wallet.subscriptions propertyTransactionId (IDOR txn — testar autenticado)

## Catálogo bruto (service -> path -> query)
"bestball.lobbyLeagueType":{"path":"/best_ball/draft_queue","query":["game_code","draft_type_id"]}
"bestball.locationCheck":{"path":"/paid_league/check_location","query":["lat","long","draft_type_id"]}
"bestball.myLeagues":{"path":"/best_ball/leagues","query":["status","start","limit"]}
"bestball.weeklyLeaderboard":{"path":"/best_ball/game/{gameCode}/league/{leagueId}/weekly_standings","query":["week"]}
"dailyfantasy.blockuser":{"path":"/blockUser","query":["crumb"]}
"dailyfantasy.checkusername":{"path":"/checkUsername","query":["username"]}
"dailyfantasy.setusername":{"path":"/username","query":["crumb"]}
"dailyfantasy.unblockuser":{"path":"/unblockUser","query":["crumb"]}
"dailyfantasy.usersettings":{"path":"/user","query":["crumb"]}
"fantasygql.leagueMeta":{"path":"/leagueMetadata","query":["gameCode","leagueId","teamId"]}
"fantasyread.getCrumb":{"path":"/getCrumb","query":["format"]}
"fantasyread.profile":{"path":"/profile_data","query":["guids","game_codes","format","season"]}
"fantasyread.weeklyfelo":{"path":"/felo_ratings/{guid}","query":["game_codes","seasons","format"]}
"fullfantasy.getCrumb":{"path":"/getCrumb","query":["format"]}
"fullfantasy.profile.settings":{"path":"/user/settings","query":["format","crumb"]}
"graphite.bettingOptionDeepLink":{"path":"/fantasyFE/nc/betslipDeepLink","query":["optionIds","stake","product","placement"]}
"graphite.bettingRestriction":{"path":"/fantasyFE/nc/bettingRestriction","query":["userStateAbbr","product","offer","placement","target"]}
"graphite.bettingSettings":{"path":"/fantasyFE/nc/bettingSettings","query":["userStateAbbr"]}
"graphite.gameDetails":{"path":"/fantasyFE/ncaabGame","query":["gameIds"]}
"graphite.gameInfo":{"path":"/fantasyFE/gameInfo","query":["gameId","ysp_src"]}
"graphite.gameOdds":{"path":"/fantasyFE/gameOdds","query":["gameId"]}
"graphite.leagueTeams":{"path":"/shangrila/leagueTeams","query":["league"]}
"graphite.redzoneGameMeta":{"path":"/fantasyFE/redzoneGameMeta","query":["gameIds"]}
"graphite.tourneyChampionOdds":{"path":"/fantasyFE/betOdds","query":["betId"]}
"progrss.contacts":{"path":"/user/{guid}/contacts;out=name,email,image;","query":["count","format","view","custId","wssid"]}
"progrss.preferences":{"path":"/user/{guid}/contacts/preferences","query":["format","custId","wssid"]}
"tourney.banGroupMember":{"path":"/v1/gql/call/tourney/banGroupMember","query":["crumb"]}
"tourney.bracketPicksLite":{"path":"/v1/gql/call/tourney/teamPicksLite","query":["teamId","teamKey"]}
"tourney.bracketPicks":{"path":"/v1/gql/call/tourney/teamPicks","query":["teamId","teamKey"]}
"tourney.bracketSettings":{"path":"/v1/gql/call/tourney/teamSettings","query":["teamId","teamKey"]}
"tourney.createBracket":{"path":"/v1/gql/call/tourney/createTeam","query":["crumb"]}
"tourney.createGroup":{"path":"/v1/gql/call/tourney/createGroup","query":["crumb"]}
"tourney.deleteBracket":{"path":"/v1/gql/call/tourney/deleteTeam","query":["crumb"]}
"tourney.dismissGroupRenewal":{"path":"/v1/gql/call/tourney/dismissGroupRenewal","query":["crumb"]}
"tourney.editBracket":{"path":"/v1/gql/call/tourney/editTeamSettings","query":["crumb"]}
"tourney.editGroupSettings":{"path":"/v1/gql/call/tourney/editGroupSettings","query":["crumb"]}
"tourney.editorialTeam":{"path":"/v1/gql/call/tourney/editorialTeam","query":["teamKey"]}
"tourney.groupMembers":{"path":"/v1/gql/call/tourney/groupMembers","query":["groupId","groupKey","start","limit"]}
"tourney.groupMyTeams":{"path":"/v1/gql/call/tourney/groupMyTeams","query":["groupId","groupKey"]}
"tourney.groupSettings":{"path":"/v1/gql/call/tourney/groupSettings","query":["groupId","groupKey","invitationKey"]}
"tourney.groups":{"path":"/v1/gql/call/tourney/fantasyGroups","query":["start","limit","teamAffiliation","groupType","publicGroupTypes","sort"]}
"tourney.groupStandings":{"path":"/v1/gql/call/tourney/groupStandings","query":["groupId","groupKey","start","limit"]}
"tourney.inviteFriends":{"path":"/v1/gql/call/tourney/groupInvitations","query":["groupId","groupKey","start","limit"]}
"tourney.joinGroup":{"path":"/v1/gql/call/tourney/joinGroup","query":["crumb"]}
"tourney.removeGroupMember":{"path":"/v1/gql/call/tourney/removeGroupMember","query":["crumb"]}
"tourney.renewedGroupMembers":{"path":"/v1/gql/call/tourney/renewedGroupMembers","query":["groupId","groupKey","start","limit"]}
"tourney.resetInvitationKey":{"path":"/v1/gql/call/tourney/resetInvitationKey","query":["crumb"]}
"tourney.sendGroupEmail":{"path":"/v1/gql/call/tourney/sendGroupEmails","query":["crumb"]}
"tourney.sendGroupInvitations":{"path":"/v1/gql/call/tourney/sendGroupInvitations","query":["crumb"]}
"tourney.sendTeamPicks":{"path":"/v1/gql/call/tourney/saveTeamPicks","query":["crumb"]}
"tourney.smartAdsGroup":{"path":"/v1/gql/call/tourney/smartAdsGroup","query":["groupId","groupKey"]}
"tourney.smartAdsTeam":{"path":"/v1/gql/call/tourney/smartAdsTeam","query":["teamId","teamKey","useLogin"]}
"unifieduser.checkusername":{"path":"/user","query":["query","variables"]}
"unifieduser.createdefault":{"path":"/user","query":["query"]}
"unifieduser.setusername":{"path":"/user","query":["query"]}
"unifieduser.sportsbookmode":{"path":"/user","query":["query"]}
"wallet.bethistory":{"path":"/sportsbook/betmgm/user/betHistory","query":["guidOverride","durationSeconds","fromDateEpochMillis","untilDateEpochMillis","count","status"]}
"wallet.bethistorysummary":{"path":"/sportsbook/betmgm/user/betHistorySummary","query":["guidOverride"]}
"wallet.product":{"path":"/product","query":["status"]}
"wallet.subscriptions":{"path":"/user/subscriptions","query":["txnType","propertyName","propertyTransactionId"]}
"wallet.validatepromocode":{"path":"/user/promotion","query":["code","type"]}
