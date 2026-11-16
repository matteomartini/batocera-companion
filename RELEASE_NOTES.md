Installer Windows (NSIS) dentro lo zip: estrai e lancia il setup.

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
