Expression 	= Term { AddOp Term } .			
Term 		= Factor { MultOp Factor } .		
Factor		= ( "(" Expression ")" ) | Number .	
AddOp 		= "+" | "-" .				
MultOp 		= "*" | "/" .				
Number 		= [ Neg ] Digit { Digit } .		
Neg		= "-" .					
Digit 		= "0" | "1" | "2" | "3" | "4" | 
	  	  "5" | "6" | "7" | "8" | "9" . 	
