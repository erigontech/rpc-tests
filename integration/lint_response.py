import os
import json
import sys
import re
import tarfile

def extract_number(filename):
    """Estrae il primo numero da una stringa per l'ordinamento."""
    match = re.search(r'\d+', filename)
    if match:
        return int(match.group())
    return 0

def formatta_response_in_json_file(filepath):
    """
    Carica un file JSON, formatta il contenuto della chiave 'response' e lo salva.
    Gestisce sia oggetti che liste come root del JSON.
    """
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            data = json.load(f)
        
        target_data = data
        if isinstance(data, list) and len(data) > 0:
            target_data = data[0]

        if 'response' in target_data and target_data['response']:
            print(f"  Elaborazione file JSON: {os.path.basename(filepath)}")
            
            response_data = target_data['response']

            if isinstance(response_data, str):
                try:
                    target_data['response'] = json.loads(response_data)
                except json.JSONDecodeError:
                    print(f"  Attenzione: La risposta in '{os.path.basename(filepath)}' non è un JSON valido. Saltato.")
                    return False
            
            with open(filepath, 'w', encoding='utf-8') as f:
                json.dump(data, f, indent=4)
            
            return True
        else:
            print(f"  Avviso: Il file '{os.path.basename(filepath)}' non contiene una chiave 'response' valida. Saltato.")
            return False

    except (json.JSONDecodeError, FileNotFoundError) as e:
        print(f"  Errore: Impossibile elaborare il file JSON '{os.path.basename(filepath)}'. Errore: {e}")
        return False

def formatta_response_in_tar_file(filepath):
    """
    Estrae un file JSON da un archivio .tar, formatta la sezione 'response'
    e ricrea l'archivio.
    """
    tar_dirname = os.path.dirname(filepath)
    tar_basename = os.path.basename(filepath)
    tar_name_without_ext = os.path.splitext(tar_basename)[0]
    
    # Crea un percorso per un file temporaneo all'interno della stessa directory
    temp_json_path = os.path.join(tar_dirname, f"{tar_name_without_ext}_temp.json")

    print(f"  Elaborazione file TAR: {tar_basename}")

    try:
        with tarfile.open(filepath, 'r:*') as tar:
            internal_json_filename = None
            for member in tar.getmembers():
                if member.name.endswith('.json'):
                    internal_json_filename = member.name
                    break
            
            if not internal_json_filename:
                print(f"  Avviso: Nessun file JSON trovato nell'archivio '{tar_basename}'. Saltato.")
                return False

            extracted_json_file_obj = tar.extractfile(internal_json_filename)
            if not extracted_json_file_obj:
                print(f"  Errore: Impossibile estrarre il file '{internal_json_filename}' dal TAR. Saltato.")
                return False

            json_content_str = extracted_json_file_obj.read().decode('utf-8')
            extracted_json_file_obj.close()
            
            # Applica la stessa logica di formattazione del file JSON
            data = json.loads(json_content_str)
            target_data = data
            if isinstance(data, list) and len(data) > 0:
                target_data = data[0]

            if 'response' in target_data and target_data['response']:
                response_data = target_data['response']
                if isinstance(response_data, str):
                    target_data['response'] = json.loads(response_data)
                
                # Scrivi il JSON modificato su un file temporaneo
                with open(temp_json_path, 'w', encoding='utf-8') as temp_f:
                    json.dump(data, temp_f, indent=4)
                
                # Ricrea l'archivio TAR con il file modificato (opzione cvj = create, verbose, j=bz2)
                with tarfile.open(filepath, 'w:bz2') as new_tar:
                    new_tar.add(temp_json_path, arcname=internal_json_filename)
                
                print(f"  **File TAR aggiornato con successo: {tar_basename}**")
                return True
            else:
                print(f"  Avviso: Il JSON interno non contiene una chiave 'response' valida. Saltato.")
                return False
                
    except (tarfile.TarError, json.JSONDecodeError, FileNotFoundError) as e:
        print(f"  Errore durante l'elaborazione del file '{tar_basename}': {e}. Salto.")
        return False
    finally:
        # Pulisci il file temporaneo
        if os.path.exists(temp_json_path):
            os.remove(temp_json_path)

def scansiona_e_formatta_directory(directory_path):
    """
    Scansiona una directory, trova i file .json e .tar e formatta la loro sezione 'response'.
    """
    if not os.path.isdir(directory_path):
        print(f"Errore: La directory '{directory_path}' non esiste.")
        return

    print(f"Avvio scansione e formattazione nella directory: {directory_path}")
    print("-" * 50)
    
    files_processati = 0
    files_aggiornati = 0
    
    # Ordina i file per nome prima di processarli
    file_list = sorted(os.listdir(directory_path), key=extract_number)

    for filename in file_list:
        filepath = os.path.join(directory_path, filename)
        if filename.endswith('.json'):
            if formatta_response_in_json_file(filepath):
                files_aggiornati += 1
            files_processati += 1
        elif filename.endswith('.tar'):
            if formatta_response_in_tar_file(filepath):
                files_aggiornati += 1
            files_processati += 1

    print("-" * 50)
    print(f"Scansione completata. File elaborati: {files_processati}. File aggiornati: {files_aggiornati}.")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Uso: python nome_script.py <percorso_directory>")
        sys.exit(1)
        
    directory_da_analizzare = sys.argv[1]
    scansiona_e_formatta_directory(directory_da_analizzare)
