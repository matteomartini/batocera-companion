Installer Windows (NSIS) dentro lo zip: estrai e lancia il setup.

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
