Installer Windows (NSIS) dentro lo zip: estrai e lancia il setup.

1.24.0
- **Tavoli Visual Pinball "pacchetto"**: certi download sono ZIP rinominati `.vpx` (TorrentZip) con dentro il tavolo vero, gli accessori e a volte la ROM: Visual Pinball non li apre. Aggiungi ROM ora li riconosce dalla firma del file, estrae il tavolo e gli accessori nella cartella del tavolo e la ROM in `pinmame/roms/`, e legge il nome della ROM dal tavolo dentro lo zip. Giochi → vpinball → "Controlla tutti i set" segnala i pacchetti già sul cabinato ("pacchetto zip, non un tavolo") e "Applica" li estrae sul posto.

1.23.6
- **Plancia**: pulsanti disposti per numero di tasto dell'encoder (0 1 2 sopra, 3 4 5 sotto: l'ordine fisico del cablaggio) con il ruolo ES sotto ciascuno; i tasti oltre il 5 (select, start, hotkey, laterali) in fila sotto, "libero" se ES non li conosce. Giocatore 1 e 2 nell'ordine assegnato in EmulationStation (Impostazioni controller), con l'etichetta "P1 in ES"; "Scambia" ora inverte davvero i pannelli.

1.23.5
- **Plancia rifatta sul serio**: i pannelli vengono dagli stessi dispositivi che il lettore eventi sta ascoltando (niente più scambi tra giocatore 1 e 2), le direzioni della leva seguono la configurazione di EmulationStation (su/giù/sinistra/destra come li ha registrati ES, anche se la leva è cablata al contrario), ogni pulsante mostra il suo numero di tasto e i tasti che l'encoder ha ma ES non conosce compaiono come "libero · tasto N", così si vede cosa manda un tasto nuovo.

1.23.4
- Space Cadet: "Aggiorna gioco e comandi" funziona anche con il gioco aperto (prima: "Text file busy").

1.23.3
- Space Cadet: il launcher scrive nel log (/userdata/system/logs/spacecadet.log) la mappatura del pad usata, per diagnosticare i tasti che il gioco non vede.

1.23.2
- Scraper per nome: l'id della scheda viene letto anche da campi alternativi della risposta di ScreenScraper; se manca ancora, l'elenco mostra i campi ricevuti.

1.23.1
- Scraper per nome: corretto "parametri mancanti" su "Usa questa"; in caso di errore il messaggio dice quale dato manca.

1.23.0
- **Scraper manuale per nome**: nei dettagli di un gioco, "🔎 Cerca su ScreenScraper per nome…" cerca la scheda (su tutti i sistemi o solo quello corrente), tu scegli quella giusta e il Companion scarica copertina, schermata, logo, video, sfondo e descrizione. Serve per i Ports (Space Cadet Pinball, Chiaki, Steam…) e per tutto ciò che lo scraper automatico non riconosce dal file.
- **Add-on → 3D Pinball - Space Cadet**: nuova scheda con "Aggiorna gioco e comandi": riscrive binario, launcher e comandi preimpostati senza toccare i file dati già sul cabinato (niente da ritrascinare).

1.22.2
- **Space Cadet Pinball con L2/R2**: Batocera passa L2/R2 come grilletti (assi) e il gioco ascolta solo i pulsanti, quindi non li vedeva. Il launcher ora li presenta come pulsanti e i default usano i tasti laterali per i flipper (sinistro: L2, Y, B; destro: R2, L1, R1). I default aggiornati vengono riapplicati al prossimo avvio; per installarli basta ritrascinare la cartella Pinball in Aggiungi ROM.

1.22.1
- **Plancia**: sotto ogni pulsante i nomi come li chiede EmulationStation quando configuri il controller: A · SUD, B · EST, X · NORD, Y · OVEST, L1, R1, L2, R2.

1.22.0
- **Alimentazione**: nuova card in Dashboard per l'interruttore Zigbee del cabinato (Sonoff su Zigbee2MQTT) tramite il broker MQTT di Home Assistant: stato della corrente, "Accendi", "Togli corrente" e "Spegni e togli corrente" (poweroff pulito di Batocera, attesa che sia giù, poi relè OFF). Nella scheda Add-on → Home Assistant si indica il topic del dispositivo (es. `zigbee2mqtt/Cabinato`) e con "Salva per l'alimentazione" i dati del broker restano sul PC senza reinstallare l'agente.

1.21.6
- **Plancia**: la configurazione di EmulationStation viene abbinata all'encoder per nome (ignorando spazi e maiuscole) e, in mancanza, per vendor:product del GUID; prima, se il nome non coincideva alla lettera, la pagina usava la prima configurazione del file (magari di un altro pad) e L2/R2 e le altre scritte non tornavano. Nell'intestazione di ogni giocatore ora si legge quale configurazione ES è in uso. Se un tasto è condiviso tra un ruolo principale e L2/R2, vince il principale.

1.21.5
- **Plancia**: i pulsanti sono disposti secondo la configurazione del controller in EmulationStation (riga alta Y X L, riga bassa B A R, poi Select/Start/Hotkey/L2/R2…) invece che per numero di porta dell'encoder. Prima, cambiando porta o riconfigurando ES, le scritte restavano sulle vecchie posizioni e si illuminava il pulsante sbagliato. Le scritte personalizzate ora sono legate al ruolo (JAB = Y, ecc.); un pulsante non configurato in ES compare tratteggiato.

1.21.4
- **Tasti per Chiaki, correzione**: il pad virtuale ora rilascia gli encoder appena Chiaki si chiude in qualsiasi modo (hotkey, crash, chiusura dall'esterno). Prima, se Chiaki usciva senza passare dal launcher, il pad restava attivo e gli encoder prigionieri: in EmulationStation i tasti risultavano tutti sballati finché non si riavviava.

1.21.3
- **Tasti per Chiaki**: in Add-on → Chiaki, "🎮 Tasti per Chiaki…" crea una mappatura usata solo da Chiaki (PS5 Remote Play), indipendente da EmulationStation. Il Companion prende gli encoder del cabinato e li presenta alla PS5 come un unico pad virtuale: ogni tasto può diventare ✕ ○ □ △ L1 R1 L2 R2 L3 R3 Create Options PS, e ogni joystick la croce, la levetta sinistra o la **levetta destra** (anche sul secondo encoder). Si assegna premendo il tasto sul cabinato.

1.21.2
- **Chiaki (PS5 Remote Play)**: il launcher in Ports chiude da solo un Chiaki rimasto appeso (prima, trovandolo, usciva subito e sembrava un crash all'avvio) e mappa il joystick del cabinato anche come **levetta sinistra**, così nei giochi PS5 il personaggio si muove (a scatti pieni: il joystick è digitale). L2/R2 arrivano dai tasti configurati come L2/R2 in EmulationStation. Per applicare: Add-on → Chiaki → Riconfigura.

1.21.1
- **Visual Pinball, ROM nel posto giusto**: Batocera 43 usa il PinMAME plugin di VPX, che cerca la ROM in `<cartella del tavolo>/pinmame/roms/` (non più in `roms/`): con il vecchio layout il tavolo partiva senza ROM (niente start, moneta, flipper). Aggiungi ROM ora mette le ROM PinMAME in `pinmame/roms/` e imposta in VPinballX.ini la cartella PinMAME comune `/userdata/roms/vpinball/pinmame` (le ROM lì valgono per tutti i tavoli). Giochi → vpinball → "Controlla tutti i set" trova le ROM nel posto sbagliato e "Applica" le sposta e sistema VPinballX.ini.

1.21.0
- **3D Pinball - Space Cadet**: trascina in Aggiungi ROM la cartella Pinball di Windows XP (o uno zip con PINBALL.DAT, .MID, SOUND*.WAV) e il Companion mette i file dati in ports/SpaceCadetPinball/ e installa il gioco in Ports (port SDL2 nativo Linux di k4zmu2a, incluso nel programma, licenza MIT) con i comandi già impostati per il cabinato: colonna sinistra dei tasti = flipper sinistro, colonna destra = flipper destro, tasti centrali = lancio pallina, joystick = scossa, Start = nuova partita, hotkey+Start = esci (F8 in gioco per cambiarli). Riconosce anche CADET.DAT di Full Tilt!.

1.20.0
- **Visual Pinball**: Aggiungi ROM riconosce i tavoli .vpx (cartella per tavolo, con .directb2s/.vbs/.ini dello stesso nome) e le ROM PinMAME (riconosciute dal DAT di MAME, verificate per CRC: se incomplete lo dice, perché il flipper non si avvia) e le mette in roms/ accanto al tavolo che le usa (letto dallo script del .vpx, anche per i tavoli già sul cabinato). Giochi → vpinball → "Controlla tutti i set": per ogni tavolo, ROM presente e completa.

1.19.9
- Aggiungi ROM: i set Model 2/3, Naomi, Atomiswave, Triforce vanno nel sistema dedicato anche se risultano "incompleti" per MAME (quegli emulatori hanno requisiti propri): copiati così come sono, con la scelta mame in alternativa.

1.19.8
- Comandi lunghi verso il cabinato (es. "Applica" su molti giochi, correzioni in blocco): lo sshd di Batocera (Dropbear) rifiuta comandi oltre ~9 KB e il Companion rispondeva "EOF". Ora i comandi lunghi vengono caricati come script temporaneo ed eseguiti da lì.

1.19.7
- DAT: se il file sorgente è vuoto o troncato (errore "EOF") viene usata la cache precedente e il messaggio dice quale DAT riscaricare dalla scheda Database.

1.19.6
- I set di schede con emulatore dedicato in Batocera (Sega Model 2 → model2, Model 3 → model3/Supermodel, Naomi/Naomi 2/Atomiswave → Flycast, Triforce → Dolphin) non vengono più messi in mame, dove non girano: Aggiungi ROM li manda nella cartella giusta, e "Controlla tutti i set" segnala quelli già in mame con "sistema sbagliato" e li sposta (con sottocartella CHD e tolto l'eventuale core per gioco).

1.19.5
- Set MAME: il controllo considera anche gli zip dei **device** richiesti da MAME moderno (es. segabill per i Model 2: "epr-18022.ic2 NOT FOUND (tried in segabill vf2)") e i **CHD** (dischi) nelle sottocartelle. Lo segnalano 🩺, "Controlla tutti i set" e il check-up; "Copia BIOS e device nelle cartelle ROM" recupera anche i device da /userdata/bios o da altre cartelle.

1.19.4
- Giochi → **Controlla tutti i set** (cartelle arcade): per ogni zip dice se è un set conosciuto, se è completo per il core in uso, per quale core lo è, se è un BIOS, se è vuoto, o se "non è questo gioco" (nome da set arcade ma nessun file corrisponde: ROM console finita in mame). Con un clic imposta il core giusto gioco per gioco (batocera.conf salvato prima).

1.19.3
- Pulsanti volume: opzione "Solo il controller su cui li ho rilevati" (porta USB): con due zero delay identici i pulsanti con lo stesso numero sull'altro encoder non toccano più il volume.

1.19.2
- **Perché non parte?** (🩺 in Giochi e nei dettagli della Libreria ROM): controlla core per gioco, confronta lo zip con i DAT di tutti i core (file mancanti per nome/CRC, BIOS presente nella cartella), avvia il gioco per 12 s e legge i log di Batocera; verdetto con causa e, dove possibile, pulsante Sistema (es. passa al core che ha il set completo, togli un core non installato).
- Set arcade: nei DAT vecchi (mame2003_plus) le ROM del BIOS non vengono più contate nel set del gioco.

1.19.1
- Versioni multiple: raggruppamento più robusto per gli arcade (parentela nei DAT, titolo del DAT, nome in EmulationStation) — trova anche i cloni con titolo diverso (es. Juuouki/Altered Beast) e i set non riconosciuti dal DAT.

1.19.0
- Libreria ROM: **stesso gioco in più versioni** (revisioni, regioni, beta, bootleg, hack): per ogni gioco propone la copia da tenere (dump riconosciuto, revisione più alta, World/USA prima di Europa sui sistemi PAL a 50 Hz e Italia/Europa sui sistemi CD, testi in italiano, niente beta/proto/hack/bootleg; per gli arcade il set principale completo, World > Europa > USA > Giappone, niente bootleg) e il pulsante "Togli le versioni in più selezionate" (cestino del cabinato). La scelta si può cambiare gioco per gioco.

1.18.9
- Libreria ROM: i duplicati (stesso CRC) sono in tabella con la scelta di quale copia togliere e il pulsante **Togli i duplicati selezionati** (cestino del cabinato).

1.18.8
- Aggiungi ROM: caricando uno zip BIOS arcade (pgm.zip, neogeo.zip…) veniva scritto uno zip vuoto, perché la ricostruzione escludeva le ROM "del BIOS" anche quando il set era il BIOS stesso. Ora viene copiato completo.

1.18.7
- Libreria ROM: errore "Cannot read properties of null" con cartelle di sistema vuote.

1.18.6
- Libreria ROM: pulsante **Copia i BIOS nelle cartelle ROM** — trova i BIOS arcade (neogeo.zip, pgm.zip, …) richiesti dai set presenti e mancanti nella cartella, e li copia da /userdata/bios o da un'altra cartella ROM; i BIOS copiati sono nascosti in EmulationStation.

1.18.5
- Libreria ROM: flag **✓ Funziona** per i set provati a mano, memorizzato anche dopo nuove analisi; i set segnati non contano più come problema nel Check-up.

1.18.4
- Libreria ROM → dettagli: per ogni set incompleto/sconosciuto i pulsanti ▶ Prova (avvia il gioco sul cabinato), ⏹ (chiude) e 🗑 (cestino del cabinato).

1.18.3
- Backup USB: "Calcola dimensioni" funziona anche sul disco collegato al cabinato (rsync a secco: totale e byte da copiare per categoria); il backup mostra avanzamento reale con categoria, MB copiati/totali, velocità e tempo mancante.
- Tutte le barre di avanzamento mostrano percentuale e stima del tempo rimanente.

1.18.2
- Backup USB: i dischi montati da Batocera in /media non comparivano perché il controllo usava `mountpoint`, assente su Batocera; ora legge /proc/mounts.

1.18.1
- Backup USB: elencati anche i dischi senza tabella partizioni e le partizioni con filesystem non riconosciuto; riquadro "Cosa vede il cabinato" con lsblk, partizioni, USB e kernel per la diagnosi.

1.18.0
- Le finestre "Scegli file" / "Scegli cartella" si aprono davanti al Companion, non più dietro.

1.17.3
- Backup su USB: i dischi collegati ma non montati da Batocera vengono elencati con un pulsante **Monta** (NTFS via ntfs3/ntfs-3g, exFAT, ext4); se il montaggio fallisce mostra il motivo e i messaggi del kernel (tipico: NTFS "sporco" per Avvio rapido di Windows o disco non espulso in sicurezza).

1.17.2
- Pulsante **Cerca il cabinato** (barra laterale e Dashboard) per riconnettersi subito; con il cabinato spento lo stato viene ricontrollato ogni 4 s. Un cabinato spento di colpo viene rilevato entro ~25 s (prima la connessione morta restava appesa per minuti).

1.17.1
- Test prestazioni in modalità **benchmark**: RetroArch (`--max-frames`, vsync/audio sbloccati) e MAME (`-bench`) danno la velocità esatta rispetto al tempo reale; misura in gioco per gli emulatori senza benchmark (Dolphin, PCSX2, Flycast…) o a richiesta.

1.17.0
- Nuova scheda **Test**: stress test temperature (CPU al 100% per N minuti, curva temperatura/frequenza, prove salvate e sovrapposte nel grafico: senza/con ventola) e test prestazioni emulatore (avvia un gioco, misura core più carico, emulatore, GPU e temperatura, verdetto e consigli; opzionale confronto A/B con le impostazioni consigliate, batocera.conf ripristinato alla fine).

1.16.6
- Emulatori: la pagina rilegge batocera.conf direttamente dopo il salvataggio; la cache delle informazioni del cabinato non viene più sovrascritta da una raccolta (lenta, 1-2 minuti per batocera-systems) iniziata prima della scrittura. Era questo a far ricomparire i valori vecchi.

1.16.5
- BIOS: i file caricati vanno nel percorso che batocera-systems richiede, non sempre in bios/ (es. segaboot.gcm del Triforce → saves/dolphin-emu/Triforce/). Il log della copia indica quando un file è stato messo altrove.

1.16.4
- Emulatori: le impostazioni per sistema ora vengono scritte davvero in batocera.conf. Il comando ufficiale `batocera-settings-set` rifiuta le chiavi assenti o commentate (caso normale in un batocera.conf di serie): il Companion ora le scrive direttamente, poi rilegge il file e mostra il risultato. Lo stesso vale per splash, Sinden e correzioni del check-up.

Dopo l'installazione aggiorna anche il server sul cabinato: Impostazioni → Server sul cabinato → Installa/aggiorna.
