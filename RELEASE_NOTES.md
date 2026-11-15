Installer Windows (NSIS) dentro lo zip: estrai e lancia il setup.

1.16.6
- Emulatori: la pagina rilegge batocera.conf direttamente dopo il salvataggio; la cache delle informazioni del cabinato non viene più sovrascritta da una raccolta (lenta, 1-2 minuti per batocera-systems) iniziata prima della scrittura. Era questo a far ricomparire i valori vecchi.

1.16.5
- BIOS: i file caricati vanno nel percorso che batocera-systems richiede, non sempre in bios/ (es. segaboot.gcm del Triforce → saves/dolphin-emu/Triforce/). Il log della copia indica quando un file è stato messo altrove.

1.16.4
- Emulatori: le impostazioni per sistema ora vengono scritte davvero in batocera.conf. Il comando ufficiale `batocera-settings-set` rifiuta le chiavi assenti o commentate (caso normale in un batocera.conf di serie): il Companion ora le scrive direttamente, poi rilegge il file e mostra il risultato. Lo stesso vale per splash, Sinden e correzioni del check-up.

Dopo l'installazione aggiorna anche il server sul cabinato: Impostazioni → Server sul cabinato → Installa/aggiorna.
